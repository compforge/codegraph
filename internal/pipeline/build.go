package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
)

type Lookup func(string) analysis.Adapter

// Analyzer owns one analysis run and its language phase barriers.
// +spec=Every language binds before any resolver runs; only this run registers contributions.
// +link=component://docs/language-pipeline.md
type Analyzer struct {
	Limits   analysis.Limits
	Lookup   Lookup
	groups   map[string]analysis.BuildScope
	adapters map[string]analysis.Adapter
	bindings []analysis.BindResult
}

func (a *Analyzer) Analyze(ctx context.Context, scope analysis.BuildScope) (*analysis.Index, []analysis.Gap, error) {
	defer func() { a.groups = nil; a.adapters = nil; a.bindings = nil }()
	var issues []analysis.Gap
	scope.Resolution, issues = language.Resolution(scope)
	index := analysis.NewBoundedIndex(scope.Files, a.Limits)
	index.Resolution = scope.Resolution
	if err := index.AddSources(ctx, scope.Names); err != nil {
		return nil, nil, err
	}
	a.groups = map[string]analysis.BuildScope{}
	groups := a.groups
	for _, p := range scope.Names {
		f := scope.Files[p]
		s := groups[f.Language]
		s.Files, s.Module, s.Resolution = scope.Files, scope.Module, scope.Resolution
		s.Names = append(s.Names, p)
		groups[f.Language] = s
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	check := func() error {
		if err := index.Err(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if index.EvidenceCount > a.Limits.Evidence {
			return analysis.ErrEvidenceLimit
		}
		if len(index.Edges) > a.Limits.Relations {
			return analysis.ErrEdgeLimit
		}
		return nil
	}
	a.adapters = map[string]analysis.Adapter{}
	adapters := a.adapters
	var organizationEdges []analysis.Edge
	for _, name := range names {
		if err := check(); err != nil {
			return nil, nil, err
		}
		adapter := a.Lookup(name)
		adapters[name] = adapter
		if adapter.Organizer == nil {
			continue
		}
		organization, err := adapter.Organizer.Organize(ctx, groups[name])
		if err != nil {
			return nil, nil, fmt.Errorf("organize %s: %w", name, err)
		}
		index.RegisterEntities(organization)
		organizationEdges = append(organizationEdges, organization.Edges...)
	}
	// All endpoints must exist before contains is indexed by the target name.
	for _, e := range organizationEdges {
		index.Add(e)
	}
	nodes := len(index.Entities)
	if nodes > a.Limits.Entities {
		return nil, nil, ErrNodeLimit
	}
	if err := index.AttachNamespaces(ctx, scope.Names); err != nil {
		return nil, nil, err
	}
	if err := index.AttachDeclarations(ctx, scope.Names); err != nil {
		return nil, nil, err
	}
	if err := check(); err != nil {
		return nil, nil, err
	}
	a.bindings = nil
	for _, name := range names {
		if err := check(); err != nil {
			return nil, nil, err
		}
		b := adapters[name].Binder
		if b == nil {
			continue
		}
		bound, err := b.Bind(ctx, groups[name], index, a.Limits.Evidence-index.EvidenceCount)
		if err != nil {
			return nil, nil, fmt.Errorf("bind %s: %w", name, a.contributionError(index, err))
		}
		for _, e := range bound.Edges {
			index.Add(e)
		}
		issues = append(issues, bound.Issues...)
		a.bindings = append(a.bindings, bound)
	}
	for _, b := range a.bindings {
		if err := check(); err != nil {
			return nil, nil, err
		}
		if b.Resolver == nil {
			continue
		}
		edges, gaps, err := b.Resolver.Resolve(ctx, index, a.Limits.Evidence-index.EvidenceCount)
		if err != nil {
			return nil, nil, a.contributionError(index, err)
		}
		for _, e := range edges {
			index.Add(e)
		}
		issues = append(issues, gaps...)
	}
	if err := check(); err != nil {
		return nil, nil, err
	}
	resourceIssues, err := index.BindResourceReferences(ctx, scope.Names)
	if err != nil {
		return nil, nil, err
	}
	issues = append(issues, resourceIssues...)
	return index, append(issues, index.Conflicts()...), nil
}

func Build(ctx context.Context, scope analysis.BuildScope, nodes, edges, evidence int, lookup Lookup) (*analysis.Index, []analysis.Gap, error) {
	a := Analyzer{Limits: analysis.Limits{Entities: nodes, Relations: edges, Evidence: evidence}, Lookup: lookup}
	return a.Analyze(ctx, scope)
}

var ErrNodeLimit = analysis.ErrEntityLimit

func Builtins(ctx context.Context, files map[string]analysis.Facts, module string, nodes, edges, evidence int) (*analysis.Index, []analysis.Gap, error) {
	return Build(ctx, analysis.NewBuildScope(files, module), nodes, edges, evidence, language.Lookup)
}

func BuiltinsWithResolution(ctx context.Context, files map[string]analysis.Facts, module string, resolution analysis.ResolutionContext, nodes, edges, evidence int) (*analysis.Index, []analysis.Gap, error) {
	scope := analysis.NewBuildScope(files, module)
	scope.Resolution = resolution
	return Build(ctx, scope, nodes, edges, evidence, language.Lookup)
}

// Language adapters receive the remaining evidence allowance for bounded
// candidate expansion. Their sentinel denotes exceeding that allowance.
func (a *Analyzer) contributionError(index *analysis.Index, err error) error {
	if errors.Is(err, analysis.ErrEdgeLimit) || errors.Is(err, analysis.ErrEvidenceLimit) {
		return &analysis.LimitError{Resource: "Evidence", Used: index.EvidenceCount, Adding: a.Limits.Evidence - index.EvidenceCount + 1, Limit: a.Limits.Evidence}
	}
	return err
}
