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
			targets := []Ref{}
			confidence, basis := "candidate", "lexical_name"
			if state := r.BindingState(); state != analysis.NotApplicable {
				if state == analysis.Bound {
					targets = append(targets, analysis.SourceRef(name, r.Target))
					confidence, basis = "exact", "lexical_binding"
				}
			} else {
				imported, matched, err := binder.useTargets(f, r.Name, r.Receiver, r.Span)
				if err != nil {
					return nil, nil, err
				}
				if matched != analysis.NotApplicable {
					for _, target := range imported {
						if len(edges) >= limit {
							return nil, nil, ErrEdgeLimit
						}
						edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target.Ref, Kind: "references", Confidence: target.Confidence, Basis: "imported_binding", Path: name, Span: r.Span})
					}
					if len(imported) == 0 {
						issues = append(issues, Issue{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: "references", Span: r.Span})
					}
					continue
				}
			}
			for _, target := range targets {
				if len(edges) >= limit {
					return nil, nil, ErrEdgeLimit
				}
				edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target, Kind: "references", Confidence: confidence, Basis: basis, Path: name, Span: r.Span})
			}
			if len(targets) == 0 {
				issues = append(issues, Issue{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: "references", Span: r.Span})
			}
		}
	}
	return edges, issues, nil
}
