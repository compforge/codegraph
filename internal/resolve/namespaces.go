package resolve

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

// Namespace is a language organization, not a universal graph label. Its key
// depends on source anchors, never the first member or the order of admission.
type Namespace struct {
	Key, Kind, Name, QualifiedName, Language string
	Documents                                []string
}

type NamespaceIndex struct {
	Units         map[string]Namespace
	ByDocument    map[string]string
	Members       map[string]map[string][]Ref
	Edges         []Edge
	goDirectories map[string][]string
	moduleFiles   map[string]extract.Facts
}

// +spec=`Namespace identities survive reordered inputs and later member or parent contributions`
func BuildNamespaces(ctx context.Context, files map[string]extract.Facts, module string) (*NamespaceIndex, error) {
	x := &NamespaceIndex{Units: map[string]Namespace{}, ByDocument: map[string]string{}, Members: map[string]map[string][]Ref{}, goDirectories: map[string][]string{}, moduleFiles: files}
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	pythonPackages := map[string][]string{}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f := files[p]
		kind, name, anchor, qualified := "", "", "", ""
		span := extract.Span{End: len(f.Source)}
		switch f.Language {
		case "go":
			if f.Package == "" {
				continue
			}
			kind, name, anchor = "Package", f.Package, path.Dir(p)
			qualified = path.Join(module, anchor)
			if module == "" {
				qualified = anchor + ":" + name
			}
			span = f.PackageSpan
			x.goDirectories[anchor] = append(x.goDirectories[anchor], p)
		case "python":
			kind, name, anchor = "Module", strings.TrimSuffix(path.Base(p), path.Ext(p)), p
			if name == "__init__" {
				kind, name, anchor = "Package", path.Base(path.Dir(p)), path.Dir(p)+"/"+path.Ext(p)
			}
			qualified = name
		default:
			continue
		}
		data, _ := json.Marshal([]string{f.Language, kind, anchor, name})
		key := string(data)
		unit := x.Units[key]
		if unit.Key == "" {
			unit = Namespace{Key: key, Kind: kind, Name: name, QualifiedName: qualified, Language: f.Language}
			x.Members[key] = map[string][]Ref{}
		}
		unit.Documents = append(unit.Documents, p)
		x.Units[key] = unit
		x.ByDocument[p] = key
		x.Edges = append(x.Edges, Edge{Ref{Path: p, Declaration: -1}, Ref{Namespace: key}, "declares", "exact", "source_namespace", p, span})
		for i, d := range f.Declarations {
			if d.Parent == -1 && d.Receiver == "" {
				x.Members[key][d.Name] = append(x.Members[key][d.Name], Ref{Path: p, Declaration: i})
			}
		}
		if f.Language == "python" && kind == "Package" {
			pythonPackages[path.Dir(p)] = append(pythonPackages[path.Dir(p)], key)
		}
	}
	// Python parenthood follows loaded package initializers, not arbitrary folder
	// nesting. .py/.pyi remain separate source candidates until a typing policy exists.
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f := files[p]
		key := x.ByDocument[p]
		if key == "" || f.Language != "python" {
			continue
		}
		dir := path.Dir(p)
		if x.Units[key].Kind == "Package" {
			if dir == "." {
				continue
			}
			dir = path.Dir(dir)
		}
		parents := pythonPackages[dir]
		for _, parent := range parents {
			confidence := "exact"
			if len(parents) > 1 {
				confidence = "candidate"
			}
			x.Edges = append(x.Edges, Edge{Ref{Namespace: parent}, Ref{Namespace: key}, "contains", confidence, "package_child", p, extract.Span{End: len(f.Source)}})
		}
		// QualifiedName is display context, not identity. Loading a parent may enrich
		// it while the module ID and all existing declaration IDs remain stable.
		parts := []string{x.Units[key].Name}
		for dir != "." && len(pythonPackages[dir]) > 0 {
			parts = append([]string{path.Base(dir)}, parts...)
			dir = path.Dir(dir)
		}
		unit := x.Units[key]
		unit.QualifiedName = strings.Join(parts, ".")
		x.Units[key] = unit
	}
	return x, nil
}

func (x *NamespaceIndex) ModuleRef(p string) Ref {
	if key := x.ByDocument[p]; key != "" {
		return Ref{Namespace: key}
	}
	return Ref{Path: p, Declaration: -1}
}

func (x *NamespaceIndex) ModulePaths(f extract.Facts, imp extract.Import) []string {
	var out []string
	for _, p := range ImportPaths(f, imp) {
		if target, ok := x.moduleFiles[p]; ok && compatibleLanguage(f.Language, target.Language) {
			out = append(out, p)
		}
	}
	return out
}

func (x *NamespaceIndex) ScopeFiles(f extract.Facts) []string {
	key := x.ByDocument[f.Path]
	if key == "" {
		return []string{f.Path}
	}
	var out []string
	for _, p := range x.Units[key].Documents {
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
	for _, p := range x.goDirectories[dir] {
		if !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	return out
}

func (x *NamespaceIndex) PackageCount(files []string) int {
	seen := map[string]bool{}
	for _, p := range files {
		seen[x.ByDocument[p]] = true
	}
	return len(seen)
}
