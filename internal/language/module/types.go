package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveTypeRelations(ctx context.Context, files map[string]analysis.Facts, names []string, module string, namespaces *NamespaceIndex, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
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
			binder := moduleBinder{ctx, files, limit - len(edges), namespaces}
			imported, matched, err := binder.useTargets(f, hint.Name, hint.Module, hint.Span)
			if err != nil {
				return nil, nil, err
			}
			targets = imported
			if matched == analysis.NotApplicable && hint.Module == "" {
				// A nested base declaration must be visible from the derived type's scope.
				scope := f.Declarations[hint.Owner].Parent
				for scope >= -1 {
					for i, d := range f.Declarations {
						if d.Parent == scope && d.Name == hint.Name && (d.Kind == "class" || d.Kind == "interface") {
							targets = append(targets, bindingTarget{Ref: analysis.SourceRef(p, i), Confidence: "exact"})
						}
					}
					if len(targets) > 0 || scope == -1 {
						break
					}
					scope = f.Declarations[scope].Parent
				}
			}
			var eligible []bindingTarget
			for _, target := range targets {
				if !target.IsDeclaration() {
					continue
				}
				kind := files[target.Path].Declarations[target.Declaration].Kind
				sourceKind := f.Declarations[hint.Owner].Kind
				allowed := kind == "class" || kind == "interface"
				if hint.Kind == "implements" || sourceKind == "interface" {
					allowed = kind == "interface"
				}
				if allowed {
					eligible = append(eligible, target)
				}
			}
			eligible = uniqueBindingTargets(eligible)
			if len(eligible) == 0 {
				issues = append(issues, Issue{Path: p, Code: "unresolved_type_relation", Reference: hint.Name, Relation: hint.Kind, Span: hint.Span})
			}
			for _, target := range eligible {
				if err := add(Edge{Source: analysis.SourceRef(p, hint.Owner), Target: target.Ref, Kind: hint.Kind, Confidence: target.Confidence, Basis: hint.Basis, Path: p, Span: hint.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	return edges, issues, nil
}
