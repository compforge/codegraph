package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

type Binder struct{ Policy Policy }
type session struct {
	scope  analysis.BuildScope
	policy Policy
}

func (b Binder) Bind(ctx context.Context, scope analysis.BuildScope, index *analysis.Index, limit int) (analysis.BindResult, error) {
	methods := &methodIndex{namespaces: &NamespaceIndex{index, b.Policy}, names: scope.Names}
	edges, issues, err := resolveTypeRelations(ctx, scope.Files, scope.Names, scope.Module, methods.namespaces, limit)
	if err != nil {
		return analysis.BindResult{}, err
	}
	for _, p := range scope.Names {
		found, gaps, err := bindImports(ctx, scope.Files[p], scope.Files, methods, limit-len(edges))
		if err != nil {
			return analysis.BindResult{}, err
		}
		edges = append(edges, found...)
		items, err := sourceItemEdges(ctx, scope.Files[p], methods.namespaces, limit-len(edges))
		if err != nil {
			return analysis.BindResult{}, err
		}
		edges = append(edges, items...)
		issues = append(issues, gaps...)
	}
	return analysis.BindResult{Edges: edges, Issues: issues, Resolver: session{scope, b.Policy}}, nil
}
func (s session) Resolve(ctx context.Context, index *analysis.Index, limit int) ([]Edge, []Issue, error) {
	methods := &methodIndex{&NamespaceIndex{index, s.policy}, s.scope.Names, analysis.NewMethodIndex(index)}
	var edges []Edge
	var issues []Issue
	for _, p := range s.scope.Names {
		found, gaps, err := resolveCalls(ctx, s.scope.Files[p], s.scope.Files, methods, limit-len(edges))
		if err != nil {
			return nil, nil, err
		}
		edges = append(edges, found...)
		issues = append(issues, gaps...)
	}
	found, gaps, err := resolveReferences(ctx, s.scope.Files, s.scope.Names, s.scope.Module, methods, limit-len(edges))
	if err != nil {
		return nil, nil, err
	}
	return append(edges, found...), append(issues, gaps...), nil
}
