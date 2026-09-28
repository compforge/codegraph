package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func bindImports(ctx context.Context, f analysis.Facts, files map[string]analysis.Facts, methods *methodIndex, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, imp := range f.Imports {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		targets := methods.namespaces.ModulePaths(f, imp)
		confidence := "exact"
		if len(targets) == 0 {
			issues = append(issues, Issue{Path: f.Path, Code: "unresolved_import", Reference: imp.Path, Relation: "imports", Span: imp.Span})
		} else if methods.namespaces.policy.ImportConfidence(imp, len(targets)) == "candidate" {
			confidence = "candidate"
		}
		for _, target := range targets {
			if err := add(Edge{Source: analysis.SourceRef(f.Path, -1), Target: methods.namespaces.Roots[target], Kind: "imports", Confidence: confidence, Basis: "source_module", Path: f.Path, Span: imp.Span}); err != nil {
				return nil, nil, err
			}
		}
	}
	binder := moduleBinder{ctx, files, limit - len(edges), methods.namespaces}
	symbolEdges, gaps, err := binder.importEdges(f)
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, symbolEdges...)
	issues = append(issues, gaps...)
	return edges, issues, nil
}
