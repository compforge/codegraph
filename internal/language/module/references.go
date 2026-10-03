package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveReferences(ctx context.Context, files map[string]analysis.Facts, names []string, module string, methods *methodIndex, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	for _, name := range names {
		f := files[name]
		binder := moduleBinder{ctx, files, limit - len(edges), methods.namespaces}
		for _, r := range f.References {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			kind := r.Kind
			if kind == "" {
				kind = "references"
			}
			targets, confidence := lexicalReferenceTargets(f, r)
			basis := "lexical_binding"
			if r.BindingState() == analysis.NotApplicable {
				imported, matched, err := binder.useTargets(f, r.Name, r.Receiver, r.Span)
				if err != nil {
					return nil, nil, err
				}
				if matched != analysis.NotApplicable {
					for _, target := range imported {
						if len(edges) >= limit {
							return nil, nil, ErrEdgeLimit
						}
						edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target.Ref, Kind: kind, Confidence: target.Confidence, Basis: "imported_binding", Path: name, Span: r.Span})
					}
					if len(imported) == 0 {
						issues = append(issues, Issue{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: kind, Span: r.Span})
					}
					continue
				}
			}
			for _, target := range targets {
				if len(edges) >= limit {
					return nil, nil, ErrEdgeLimit
				}
				edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target, Kind: kind, Confidence: confidence, Basis: basis, Path: name, Span: r.Span})
			}
			if len(targets) == 0 {
				issues = append(issues, Issue{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: kind, Span: r.Span})
			}
		}
	}
	return edges, issues, nil
}
