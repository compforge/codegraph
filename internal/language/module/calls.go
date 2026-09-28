package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveCalls(ctx context.Context, f analysis.Facts, files map[string]analysis.Facts, methods *methodIndex, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	binder := moduleBinder{ctx, files, limit, methods.namespaces}
	for _, call := range f.Calls {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		source := analysis.SourceRef(f.Path, -1)
		best := len(f.Source) + 1
		var targets []Ref
		for i, d := range f.Declarations {
			if d.Start <= call.Start && d.End >= call.End && d.End-d.Start < best {
				source = analysis.DeclarationRef(f.Path, i)
				best = d.End - d.Start
			}
			if d.Parent == -1 && d.Kind == "function" && d.Name == call.Name {
				targets = append(targets, analysis.SourceRef(f.Path, i))
			}
		}
		supplement, err := resolveCallTargets(ctx, f, call, files, "", methods, limit-len(edges))
		if err != nil {
			return nil, nil, err
		}
		if len(supplement) > 0 {
			edges = append(edges, supplement...)
			continue
		}
		if call.Imported {
			imported, _, err := binder.useTargets(f, call.Name, call.Receiver, call.Span)
			if err != nil {
				return nil, nil, err
			}
			found := false
			for _, target := range imported {
				if !target.IsDeclaration() || files[target.Path].Declarations[target.Declaration].Kind != "function" {
					continue
				}
				found = true
				if err := add(Edge{Source: source, Target: target.Ref, Kind: "calls", Confidence: target.Confidence, Basis: "imported_binding", Path: f.Path, Span: call.Span}); err != nil {
					return nil, nil, err
				}
			}
			if !found {
				issues = append(issues, Issue{Path: f.Path, Code: "unresolved_call", Reference: call.Name, Relation: "calls", Span: call.Span})
			}
			continue
		}
		if call.Blocked || call.Receiver != "" {
			issues = append(issues, Issue{Path: f.Path, Code: "dynamic_call", Reference: call.Name, Relation: "calls", Span: call.Span})
			continue
		}
		if len(targets) == 0 {
			issues = append(issues, Issue{Path: f.Path, Code: "unresolved_call", Reference: call.Name, Relation: "calls", Span: call.Span})
			continue
		}
		confidence := analysis.Exact
		if len(targets) > 1 {
			confidence = "scoped"
		}
		for _, target := range targets {
			if err := add(Edge{Source: source, Target: target, Kind: "calls", Confidence: confidence, Basis: "module_function", Path: f.Path, Span: call.Span}); err != nil {
				return nil, nil, err
			}
		}
	}
	return edges, issues, nil
}
