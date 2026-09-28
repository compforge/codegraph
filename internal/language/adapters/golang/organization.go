package golang

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/compforge/codegraph/internal/analysis"
)

func ImportDir(module, imp string) (string, bool) {
	if module == "" {
		return "", false
	}
	if imp == module {
		return ".", true
	}
	if strings.HasPrefix(imp, module+"/") {
		return strings.TrimPrefix(imp, module+"/"), true
	}
	return "", false
}

func exported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

type NamespaceIndex struct {
	*analysis.Index
	directories map[string][]string
}

func newNamespaces(index *analysis.Index) *NamespaceIndex {
	x := &NamespaceIndex{Index: index, directories: map[string][]string{}}
	for p, f := range index.Files {
		if f.Language == "go" {
			dir := path.Dir(p)
			x.directories[dir] = append(x.directories[dir], p)
		}
	}
	for _, paths := range x.directories {
		sort.Strings(paths)
	}
	return x
}

func (Adapter) Organize(ctx context.Context, scope analysis.Scope) (analysis.Organization, error) {
	units := map[string]analysis.Entity{}
	roots := map[string]analysis.Ref{}
	var edges []Edge
	for _, p := range scope.Names {
		if err := ctx.Err(); err != nil {
			return analysis.Organization{}, err
		}
		f := scope.Files[p]
		if f.Package == "" {
			continue
		}
		anchor := path.Dir(p)
		qualified := path.Join(scope.Module, anchor)
		if scope.Module == "" {
			qualified = anchor + ":" + f.Package
		}
		data, _ := json.Marshal([]string{f.Language, "Package", anchor, f.Package})
		key := string(data)
		u := units[key]
		if u.Kind == "" {
			u = analysis.Entity{Ref: analysis.SyntheticRef(key), Kind: "Package", Name: f.Package, QualifiedName: qualified, Language: f.Language}
		}
		roots[p] = u.Ref
		units[key] = u
		edges = append(edges, Edge{Source: analysis.DocumentRef(p), Target: analysis.SyntheticRef(key), Kind: "declares", Confidence: "exact", Basis: "source_namespace", Path: p, Span: f.PackageSpan})
	}
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]analysis.Entity, 0, len(keys))
	for _, k := range keys {
		out = append(out, units[k])
	}
	return analysis.Organization{Entities: out, Roots: roots, Edges: edges}, nil
}
func (x *NamespaceIndex) ScopeFiles(f analysis.Facts) []string {
	root, ok := x.Roots[f.Path]
	if !ok {
		return []string{f.Path}
	}
	var out []string
	for _, contribution := range x.Namespace(root).Contributions() {
		p := contribution.Path
		if f.Language != "go" || !strings.HasSuffix(p, "_test.go") || strings.HasSuffix(f.Path, "_test.go") {
			out = append(out, p)
		}
	}
	return out
}

func (x *NamespaceIndex) GoImportFiles(module, imported string) []string {
	dir, ok := ImportDir(module, imported)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range x.directories[dir] {
		if !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	return out
}

func importedFiles(f analysis.Facts, namespaces *NamespaceIndex, module string) map[string][]string {
	imports := map[string][]string{}
	for _, imp := range f.Imports {
		for _, p := range namespaces.GoImportFiles(module, imp.Path) {
			alias := imp.Alias
			if alias == "" {
				alias = namespaces.Files[p].Package
			}
			imports[alias] = append(imports[alias], p)
		}
	}
	return imports
}

func (x *NamespaceIndex) PackageCount(paths []string) int {
	seen := map[analysis.Ref]bool{}
	for _, p := range paths {
		if root, ok := x.Roots[p]; ok {
			seen[root] = true
		}
	}
	return len(seen)
}
