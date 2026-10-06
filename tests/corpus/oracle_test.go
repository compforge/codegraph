package corpus_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	cg "github.com/compforge/codegraph"
	"golang.org/x/tools/go/packages"
)

type inputFile struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	SHA256 string `json:"sha256"`
}

func loadOracle(ctx context.Context, root string) (*oracle, []cg.Document, []inputFile, error) {
	return loadGoModulesOracle(ctx, root, ".")
}

// Module roots are explicit evaluation inputs; the compiler owns module paths
// and bindings. The existing repository profile still loads only its root.
func loadGoModulesOracle(ctx context.Context, root string, moduleRoots ...string) (*oracle, []cg.Document, []inputFile, error) {
	// Tests are a separate package universe in Go. The first profile deliberately
	// measures production packages, rather than mixing test variants into a graph.
	env := []string{}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		switch key {
		case "GOWORK", "GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED":
			continue
		}
		env = append(env, e)
	}
	env = append(env, "GOWORK=off", "GOFLAGS=", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	mode := packages.LoadSyntax | packages.NeedModule
	if len(moduleRoots) > 1 {
		// Imports of another evaluated module need source positions, not export
		// data's line-only positions. These small fixtures have no external deps.
		mode = packages.LoadAllSyntax | packages.NeedModule
	}
	var pkgs []*packages.Package
	for _, moduleRoot := range moduleRoots {
		loaded, err := packages.Load(&packages.Config{Context: ctx, Dir: filepath.Join(root, moduleRoot), Env: env,
			Mode: mode, BuildFlags: []string{"-mod=readonly"}}, "./...")
		if err != nil {
			return nil, nil, nil, err
		}
		if len(loaded) == 0 {
			return nil, nil, nil, fmt.Errorf("oracle found no packages in %s", moduleRoot)
		}
		pkgs = append(pkgs, loaded...)
	}
	var problems []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			problems = append(problems, e.Error())
		}
	})
	if len(problems) > 0 {
		return nil, nil, nil, fmt.Errorf("oracle package loading failed:\n%s", strings.Join(problems, "\n"))
	}
	if len(pkgs) == 0 {
		return nil, nil, nil, fmt.Errorf("oracle found no packages")
	}
	o := &oracle{Organizations: map[string]organization{}, Owners: map[string]string{}, Declarations: map[string]declaration{}, References: map[string]occurrence{}, Calls: map[string]occurrence{}, Imports: map[string]occurrence{}, ExcludedReferences: map[string]occurrence{}}
	documents := map[string]cg.Document{}
	for _, p := range pkgs {
		if p.Module == nil || p.TypesInfo == nil {
			return nil, nil, nil, fmt.Errorf("missing compiler facts for %s", p.ID)
		}
		if len(moduleRoots) == 1 && o.Module == "" {
			o.Module = p.Module.Path
		}
		if len(moduleRoots) == 1 && o.Module != p.Module.Path {
			return nil, nil, nil, fmt.Errorf("multiple modules require separate profiles")
		}
		o.Organizations["module:"+p.Module.Path] = organization{Kind: cg.Module, Name: p.Module.Path, QualifiedName: p.Module.Path}
		for _, file := range p.Syntax {
			abs := p.Fset.PositionFor(file.Pos(), false).Filename
			rel, err := filepath.Rel(root, abs)
			if err != nil || !filepath.IsLocal(rel) {
				return nil, nil, nil, fmt.Errorf("non-source compiler file: %s", abs)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return nil, nil, nil, err
			}
			rel = filepath.ToSlash(rel)
			documents[rel] = cg.Document{Path: rel, Content: data}
			collectDeclarations(o, p, file, root)
			unit := o.Organizations[p.PkgPath]
			if unit.Contributions == nil {
				unit = organization{Kind: cg.Package, Name: p.Name, QualifiedName: p.PkgPath, Contributions: map[string]site{}, Parent: "module:" + p.Module.Path}
			}
			unit.Contributions[rel] = sourceSite(p.Fset, root, file.Package, file.Name.End())
			o.Organizations[p.PkgPath] = unit
		}
	}
	// Uses can point to a declaration in a later package; collect all definitions first.
	for _, p := range pkgs {
		for _, file := range p.Syntax {
			collectOccurrences(o, p, file, root)
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
					obj, _ := p.TypesInfo.Defs[fn.Name].(*types.Func)
					if obj == nil {
						continue
					}
					recv := obj.Type().(*types.Signature).Recv().Type()
					if ptr, ok := recv.(*types.Pointer); ok {
						recv = ptr.Elem()
					}
					if named, ok := recv.(*types.Named); ok {
						target, _ := objectTarget(o, p, root, named.Obj())
						o.Owners[sourceSite(p.Fset, root, fn.Name.Pos(), fn.Name.End()).key()] = target
					}
				}
			}
		}
	}
	var docs []cg.Document
	for _, doc := range documents {
		docs = append(docs, doc)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	var inventory []inputFile
	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		state := "excluded_by_go_package_profile"
		if _, ok := documents[rel]; ok {
			state = "included"
		} else if strings.HasSuffix(rel, "_test.go") {
			state = "test_file"
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		inventory = append(inventory, inputFile{rel, state, fmt.Sprintf("%x", sha256.Sum256(data))})
		return nil
	})
	completeOrganizations(o, docs)
	return o, docs, inventory, err
}

func sourceSite(fset *token.FileSet, root string, start, end token.Pos) site {
	p := fset.PositionFor(start, false)
	rel, _ := filepath.Rel(root, p.Filename)
	return site{filepath.ToSlash(rel), p.Offset, fset.PositionFor(end, false).Offset}
}

func collectDeclarations(o *oracle, p *packages.Package, file *ast.File, root string) {
	add := func(id *ast.Ident, start, end token.Pos, kind cg.NodeKind, supported bool, reason string) {
		if id.Name == "_" {
			return
		}
		n := sourceSite(p.Fset, root, id.Pos(), id.End())
		o.Declarations[n.key()] = declaration{id.Name, kind, n, sourceSite(p.Fset, root, start, end), supported, reason}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			kind := cg.Function
			if x.Recv != nil {
				kind = cg.Method
			}
			add(x.Name, x.Pos(), x.End(), kind, true, "")
		case *ast.TypeSpec:
			kind := cg.Type
			switch x.Type.(type) {
			case *ast.StructType:
				kind = cg.Struct
			case *ast.InterfaceType:
				kind = cg.Interface
			}
			if x.Assign.IsValid() {
				kind = cg.TypeAlias
			}
			add(x.Name, x.Pos(), x.End(), kind, true, "")
			var fields *ast.FieldList
			memberKind := cg.Field
			switch typ := x.Type.(type) {
			case *ast.StructType:
				fields = typ.Fields
			case *ast.InterfaceType:
				fields, memberKind = typ.Methods, cg.Method
			}
			if fields != nil {
				for _, field := range fields.List {
					for _, id := range field.Names {
						add(id, id.Pos(), field.End(), memberKind, true, "")
					}
					if len(field.Names) == 0 && memberKind == cg.Field {
						// The compiler associates an embedded field with its type identifier.
						ast.Inspect(field.Type, func(n ast.Node) bool {
							if id, ok := n.(*ast.Ident); ok {
								if obj, ok := p.TypesInfo.Defs[id].(*types.Var); ok && obj.IsField() {
									add(id, field.Pos(), field.End(), cg.Field, true, "")
								}
							}
							return true
						})
					}
				}
			}
		case *ast.GenDecl:
			if x.Tok != token.VAR && x.Tok != token.CONST {
				break
			}
			kind := cg.Variable
			if x.Tok == token.CONST {
				kind = cg.Constant
			}
			for _, spec := range x.Specs {
				v := spec.(*ast.ValueSpec)
				for _, id := range v.Names {
					reason := ""
					if len(v.Names) > 1 {
						reason = "multi_name_value_declaration"
					}
					add(id, id.Pos(), v.End(), kind, reason == "", reason)
				}
			}
		}
		return true
	})
	// Keep parameters, short declarations and anonymous members in the overall
	// denominator, even though the current graph contract does not model them.
	for id, obj := range p.TypesInfo.Defs {
		if id.Pos() < file.Pos() || id.Pos() >= file.End() || obj == nil || id.Name == "_" {
			continue
		}
		kind := cg.Variable
		switch obj.(type) {
		case *types.PkgName, *types.Label:
			continue
		case *types.TypeName:
			kind = cg.Type
		case *types.Func:
			kind = cg.Method
		case *types.Const:
			kind = cg.Constant
		case *types.Var:
			if obj.(*types.Var).IsField() {
				kind = cg.Field
			}
		}
		key := sourceSite(p.Fset, root, id.Pos(), id.End()).key()
		if _, exists := o.Declarations[key]; !exists {
			add(id, id.Pos(), id.End(), kind, false, "non_graph_binding")
		}
	}
}

func objectTarget(o *oracle, p *packages.Package, root string, obj types.Object) (string, string) {
	if obj == nil {
		return "", "unknown"
	}
	if f, ok := obj.(*types.Func); ok {
		obj = f.Origin()
	}
	if v, ok := obj.(*types.Var); ok {
		obj = v.Origin()
	}
	if !obj.Pos().IsValid() {
		return "", "builtin"
	}
	s := sourceSite(p.Fset, root, obj.Pos(), obj.Pos()+token.Pos(len(obj.Name())))
	d, ok := o.Declarations[s.key()]
	if !ok {
		return "", "external"
	}
	if !d.Supported {
		return s.key(), "outside_graph_contract"
	}
	return s.key(), "internal"
}

func collectOccurrences(o *oracle, p *packages.Package, file *ast.File, root string) {
	for id, obj := range p.TypesInfo.Uses {
		if id.Pos() < file.Pos() || id.Pos() >= file.End() {
			continue
		}
		s := sourceSite(p.Fset, root, id.Pos(), id.End())
		target, class := objectTarget(o, p, root, obj)
		if _, ok := obj.(*types.PkgName); ok {
			class = "package_qualifier"
		}
		r := occurrence{Site: s, Name: id.Name, Target: target, Class: class}
		if class == "builtin" || class == "package_qualifier" {
			o.ExcludedReferences[s.key()] = r
		} else {
			o.References[s.key()] = r
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			name, _ := strconv.Unquote(x.Path.Value)
			s := sourceSite(p.Fset, root, x.Pos(), x.End())
			class := "external"
			if _, included := o.Organizations[name]; included || o.Module != "" && (name == o.Module || strings.HasPrefix(name, o.Module+"/")) {
				class = "internal"
			}
			o.Imports[s.key()] = occurrence{Site: s, Name: name, Target: name, Class: class}
		case *ast.CallExpr:
			s := sourceSite(p.Fset, root, x.Pos(), x.End())
			r := occurrence{Site: s, Name: "call", Class: "runtime_dispatch"}
			fun := x.Fun
			for {
				switch y := fun.(type) {
				case *ast.IndexExpr:
					fun = y.X
				case *ast.IndexListExpr:
					fun = y.X
				case *ast.ParenExpr:
					fun = y.X
				default:
					goto unwrapped
				}
			}
		unwrapped:
			var obj types.Object
			dynamic := false
			switch y := fun.(type) {
			case *ast.Ident:
				obj, r.Name = p.TypesInfo.Uses[y], y.Name
			case *ast.SelectorExpr:
				obj, r.Name = p.TypesInfo.Uses[y.Sel], y.Sel.Name
				if selection := p.TypesInfo.Selections[y]; selection != nil {
					_, dynamic = selection.Recv().Underlying().(*types.Interface)
				}
			}
			if p.TypesInfo.Types[x.Fun].IsType() {
				r.Class = "type_conversion"
			} else if _, ok := obj.(*types.Builtin); ok {
				r.Class = "builtin"
			} else if _, ok := obj.(*types.Func); ok && !dynamic {
				r.Target, r.Class = objectTarget(o, p, root, obj)
			}
			o.Calls[s.key()] = r
		}
		return true
	})
}
