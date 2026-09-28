package golang

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func goTypeTargets(f analysis.Facts, name, qualifier string, files map[string]analysis.Facts, namespaces *NamespaceIndex, module string) []bindingTarget {
	paths := namespaces.ScopeFiles(f)
	if qualifier != "" {
		paths = nil
		for _, imp := range f.Imports {
			for _, p := range namespaces.GoImportFiles(module, imp.Path) {
				alias := imp.Alias
				if alias == "" {
					alias = files[p].Package
				}
				if alias == qualifier {
					paths = append(paths, p)
				}
			}
		}
	}
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[p] = true
	}
	seen := map[analysis.Ref]bool{}
	var out []bindingTarget
	for _, p := range paths {
		key := namespaces.Roots[p]
		if seen[key] {
			continue
		}
		seen[key] = true
		for _, ref := range namespaces.Namespace(key).Members(name) {
			if !allowed[ref.Path] || qualifier != "" && !exported(name) {
				continue
			}
			switch files[ref.Path].Declarations[ref.Declaration].Kind {
			case "struct", "interface", "type", "type_alias":
				out = append(out, bindingTarget{Ref: ref, Confidence: "exact"})
			}
		}
	}
	return uniqueBindingTargets(out)
}

func resolveTypeRelations(ctx context.Context, files map[string]analysis.Facts, names []string, module string, namespaces *NamespaceIndex, limit int) ([]Edge, []Gap, error) {
	var edges []Edge
	var issues []Gap
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := files[p]
		for _, hint := range f.TypeRelations {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			var targets []bindingTarget
			targets = goTypeTargets(f, hint.Name, hint.Module, files, namespaces, module)
			var eligible []bindingTarget
			for _, target := range targets {
				if !target.IsDeclaration() {
					continue
				}
				kind := files[target.Path].Declarations[target.Declaration].Kind
				sourceKind := f.Declarations[hint.Owner].Kind
				allowed := kind == "class" || kind == "interface"
				if f.Language == "go" || hint.Kind == "implements" || sourceKind == "interface" {
					allowed = kind == "interface"
				}
				if allowed {
					eligible = append(eligible, target)
				}
			}
			eligible = uniqueBindingTargets(eligible)
			if len(eligible) == 0 {
				issues = append(issues, Gap{Path: p, Code: "unresolved_type_relation", Reference: hint.Name, Relation: hint.Kind, Span: hint.Span})
			}
			for _, target := range eligible {
				if err := add(Edge{Source: analysis.SourceRef(p, hint.Owner), Target: target.Ref, Kind: hint.Kind, Confidence: target.Confidence, Basis: hint.Basis, Path: p, Span: hint.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
		if f.Language != "go" {
			continue
		}
		for i, d := range f.Declarations {
			if d.Parent != -1 || d.Kind != "struct" && d.Kind != "type" {
				continue
			}
			methods := map[string]bool{}
			for _, q := range names {
				other := files[q]
				if other.Language != "go" || path.Dir(q) != path.Dir(p) || other.Package != f.Package || strings.HasSuffix(q, "_test.go") && !strings.HasSuffix(p, "_test.go") {
					continue
				}
				for _, m := range other.Declarations {
					if m.Kind == "method" && m.Receiver == d.Name {
						methods[m.Name] = true
					}
				}
			}
			for _, q := range names {
				other := files[q]
				if other.Language != "go" || path.Dir(q) != path.Dir(p) || other.Package != f.Package || strings.HasSuffix(q, "_test.go") && !strings.HasSuffix(p, "_test.go") {
					continue
				}
				for j, iface := range other.Declarations {
					if err := ctx.Err(); err != nil {
						return nil, nil, err
					}
					if iface.Kind != "interface" || iface.Parent != -1 {
						continue
					}
					// Empty interfaces and embedded/type-term interfaces would yield overly
					// broad matches from this direct-method vocabulary; exclude them from inference.
					complete := true
					for _, h := range other.TypeRelations {
						if h.Owner == j {
							complete = false
						}
					}
					for _, issue := range other.Issues {
						if issue.Relation == "extends" && issue.Start >= iface.Start && issue.End <= iface.End {
							complete = false
						}
					}
					count := 0
					for _, m := range other.Declarations {
						if m.Kind == "method" && m.Parent == j {
							count++
							if !methods[m.Name] {
								complete = false
							}
						}
					}
					if !complete || count == 0 {
						continue
					}
					if err := add(Edge{Source: analysis.SourceRef(p, i), Target: analysis.SourceRef(q, j), Kind: "implements", Confidence: analysis.Heuristic, Basis: "method_name_set", Path: p, Span: d.Span}); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}
	return edges, issues, nil
}
