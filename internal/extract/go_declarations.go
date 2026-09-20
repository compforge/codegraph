package extract

import (
	"go/ast"
	"go/token"
	"sort"
)

// FactProgram supplies declaration spans but groups Go types together and does
// not emit fields, interface methods, aliases, variables, or constants. Supplement those facts here;
// do not expose parser node names as the public graph's category vocabulary.
func enrichGoDeclarations(f *Facts, file *ast.File, fset *token.FileSet) {
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	decls := map[int]int{}
	for i, d := range f.Declarations {
		decls[d.Start] = i
	}
	upsertSpan := func(name, kind string, start, end int, doc *ast.CommentGroup) int {
		i, ok := decls[start]
		if !ok {
			i = len(f.Declarations)
			decls[start] = i
			f.Declarations = append(f.Declarations, Declaration{})
		}
		f.Declarations[i] = Declaration{Name: name, Kind: kind, Span: Span{start, end}, Comments: comments(doc, fset)}
		return i
	}
	upsert := func(name, kind string, n ast.Node, doc *ast.CommentGroup) int {
		return upsertSpan(name, kind, offset(n.Pos()), offset(n.End()), doc)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			kind, receiver := "function", ""
			if n.Recv != nil && len(n.Recv.List) > 0 {
				kind, receiver = "method", receiverName(n.Recv.List[0].Type)
			}
			i := upsert(n.Name.Name, kind, n, n.Doc)
			f.Declarations[i].Receiver = receiver
		case *ast.GenDecl:
			for _, spec := range n.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					doc := declarationDoc(spec.Doc, n.Doc, len(n.Specs))
					kind := "type"
					switch spec.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					if spec.Assign.IsValid() {
						kind = "type_alias"
					}
					upsert(spec.Name.Name, kind, spec, doc)
					switch typ := spec.Type.(type) {
					case *ast.StructType:
						appendGoMembers(f, typ.Fields, "field", fset)
					case *ast.InterfaceType:
						appendGoMembers(f, typ.Methods, "method", fset)
					}
				case *ast.ValueSpec:
					if len(spec.Names) != 1 || n.Tok != token.CONST && n.Tok != token.VAR {
						continue
					}
					kind := "variable"
					if n.Tok == token.CONST {
						kind = "constant"
					}
					doc := declarationDoc(spec.Doc, n.Doc, len(n.Specs))
					upsertSpan(spec.Names[0].Name, kind, offset(spec.Names[0].Pos()), offset(spec.End()), doc)
				}
			}
		}
		return true
	})
	assignDeclarationParents(f.Declarations)
}

func declarationDoc(spec, group *ast.CommentGroup, groupSize int) *ast.CommentGroup {
	if spec == nil && groupSize == 1 {
		return group
	}
	return spec
}

func appendGoMembers(f *Facts, fields *ast.FieldList, kind string, fset *token.FileSet) {
	for _, field := range fields.List {
		var names []string
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
		if len(names) == 0 && kind == "field" {
			// Embedded struct fields declare a field with the unqualified type name.
			// Interface embeddings, in contrast, do not declare a new method.
			names = append(names, embeddedFieldName(field.Type))
		}
		for i, name := range names {
			start := field.Pos()
			if len(field.Names) > 0 {
				start = field.Names[i].Pos()
			}
			f.Declarations = append(f.Declarations, Declaration{
				Name: name, Kind: kind,
				Span:     Span{fset.Position(start).Offset, fset.Position(field.End()).Offset},
				Comments: comments(field.Doc, fset),
			})
		}
	}
}

func embeddedFieldName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return embeddedFieldName(e.X)
	case *ast.IndexExpr:
		return embeddedFieldName(e.X)
	case *ast.IndexListExpr:
		return embeddedFieldName(e.X)
	default:
		return receiverName(e)
	}
}

// Strict span containment gives lexical ownership, not receiver ownership.
// Fields in a shared declaration may have overlapping spans but never introduce
// declaration scopes. Each starts at its own name so even blank fields have
// distinct identities. Equal spans are likewise never each other's parent.
func assignDeclarationParents(decls []Declaration) {
	sort.Slice(decls, func(i, j int) bool {
		a, b := decls[i], decls[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End > b.End
		}
		return a.Name < b.Name
	})
	var stack []int
	for i := range decls {
		d := &decls[i]
		d.Parent, d.QualifiedName = -1, d.Name
		for len(stack) > 0 {
			p := decls[stack[len(stack)-1]]
			if p.Start <= d.Start && p.End >= d.End && p.End-p.Start > d.End-d.Start {
				break
			}
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			d.Parent = stack[len(stack)-1]
			d.QualifiedName = decls[d.Parent].QualifiedName + "." + d.Name
		} else if d.Receiver != "" {
			d.QualifiedName = d.Receiver + "." + d.Name
		}
		if d.Kind != "field" {
			stack = append(stack, i)
		}
	}
}
