package python

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func (Adapter) Organize(ctx context.Context, scope analysis.BuildScope) (analysis.Organization, error) {
	files := scope.Files
	units := map[string]analysis.Entity{}
	roots := map[string]string{}
	var edges []analysis.Edge
	names := scope.Names
	pythonPackages := map[string][]string{}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return analysis.Organization{}, err
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
		unit := units[key]
		if unit.Kind == "" {
			unit = analysis.Entity{Ref: analysis.SyntheticRef(key), Kind: kind, Name: name, QualifiedName: qualified, Language: f.Language}
		}
		units[key] = unit
		roots[p] = key
		edges = append(edges, analysis.Edge{Source: analysis.SourceRef(p, -1), Target: analysis.SyntheticRef(key), Kind: "declares", Confidence: "exact", Basis: "source_namespace", Path: p, Span: span})
		if f.Language == "python" && kind == "Package" {
			pythonPackages[path.Dir(p)] = append(pythonPackages[path.Dir(p)], key)
		}
	}
	// Python parenthood follows loaded package initializers, not arbitrary folder
	// nesting. .py/.pyi remain separate source candidates until a typing policy exists.
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return analysis.Organization{}, err
		}
		f := files[p]
		key := roots[p]
		if key == "" || f.Language != "python" {
			continue
		}
		dir := path.Dir(p)
		if units[key].Kind == "Package" {
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
			edges = append(edges, analysis.Edge{Source: analysis.SyntheticRef(parent), Target: analysis.SyntheticRef(key), Kind: "contains", Confidence: confidence, Basis: "package_child", Path: p, Span: analysis.Span{End: len(f.Source)}})
		}
		// QualifiedName is display context, not identity. Loading a parent may enrich
		// it while the module ID and all existing declaration IDs remain stable.
		parts := []string{units[key].Name}
		for dir != "." && len(pythonPackages[dir]) > 0 {
			parts = append([]string{path.Base(dir)}, parts...)
			dir = path.Dir(dir)
		}
		unit := units[key]
		unit.QualifiedName = strings.Join(parts, ".")
		units[key] = unit
	}
	result := analysis.Organization{Roots: map[string]analysis.Ref{}, Edges: edges}
	for _, p := range names {
		unit := units[roots[p]]
		result.Entities = append(result.Entities, unit)
		result.Roots[p] = unit.Ref
	}
	return result, nil
}
