package python

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func (Adapter) Organize(ctx context.Context, scope analysis.Scope) ([]analysis.Organization, []analysis.Edge, error) {
	files := scope.Files
	x := analysis.NewIndex(files)
	names := scope.Names
	pythonPackages := map[string][]string{}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := files[p]
		kind, name, anchor, qualified := "", "", "", ""
		span := analysis.Span{End: len(f.Source)}
		kind, name, anchor = "Module", strings.TrimSuffix(path.Base(p), path.Ext(p)), p
		if name == "__init__" {
			kind, name, anchor = "Package", path.Base(path.Dir(p)), path.Dir(p)+"/"+path.Ext(p)
		}
		qualified = name
		data, _ := json.Marshal([]string{f.Language, kind, anchor, name})
		key := string(data)
		unit := x.Units[key]
		if unit.Key == "" {
			unit = analysis.Organization{Key: key, Kind: kind, Name: name, QualifiedName: qualified, Language: f.Language}
		}
		unit.Documents = append(unit.Documents, p)
		x.Units[key] = unit
		x.ByDocument[p] = key
		x.Edges = append(x.Edges, analysis.Edge{Source: analysis.SourceRef(p, -1), Target: analysis.OrganizationRef(key), Kind: "declares", Confidence: "exact", Basis: "source_namespace", Path: p, Span: span})
		if f.Language == "python" && kind == "Package" {
			pythonPackages[path.Dir(p)] = append(pythonPackages[path.Dir(p)], key)
		}
	}
	// Python parenthood follows loaded package initializers, not arbitrary folder
	// nesting. .py/.pyi remain separate source candidates until a typing policy exists.
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
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
			x.Edges = append(x.Edges, analysis.Edge{Source: analysis.OrganizationRef(parent), Target: analysis.OrganizationRef(key), Kind: "contains", Confidence: confidence, Basis: "package_child", Path: p, Span: analysis.Span{End: len(f.Source)}})
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
	units := make([]analysis.Organization, 0, len(x.Units))
	for _, p := range names {
		units = append(units, x.Units[x.ByDocument[p]])
	}
	return units, x.Edges, nil
}
