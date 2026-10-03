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
		source := analysis.SourceRef(f.Path, analysis.EnclosingDeclaration(f, call.Span))
		var targets []Ref
		confidence := analysis.Exact
		// Calls and references share the same lexical proof. Missing retained
		// parents or a same-spelled module member cannot establish visibility.
		for _, ref := range f.References {
			if ref.Start < call.Start || ref.End > call.End || ref.Name != call.Name || ref.Receiver != call.Receiver {
				continue
			}
			bound, precision := lexicalReferenceTargets(f, ref)
			confidence = precision
			for _, target := range bound {
				if f.Declarations[target.Declaration].Kind == "function" {
					targets = append(targets, target)
				}
			}
			break
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
		if call.Blocked || call.Receiver != "" && len(targets) == 0 {
			issues = append(issues, Issue{Path: f.Path, Code: "dynamic_call", Reference: call.Name, Relation: "calls", Span: call.Span})
			continue
		}
		if len(targets) == 0 {
			issues = append(issues, Issue{Path: f.Path, Code: "unresolved_call", Reference: call.Name, Relation: "calls", Span: call.Span})
			continue
		}
		for _, target := range targets {
			if err := add(Edge{Source: source, Target: target, Kind: "calls", Confidence: confidence, Basis: "lexical_binding", Path: f.Path, Span: call.Span}); err != nil {
				return nil, nil, err
			}
		}
	}
	return edges, issues, nil
}
