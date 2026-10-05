package pipeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
)

type Lookup func(string) analysis.Adapter

// Build owns phase barriers: every language binds before any language resolves.
// +why=`A later document or language batch may supply a base type needed by inherited member lookup`
func Build(ctx context.Context, scope analysis.BuildScope, nodeLimit, edgeLimit, evidenceLimit int, lookup Lookup) (*analysis.Index, []analysis.Gap, error) {
	scope.Resolution = scope.Resolution.WithGoModule(scope.Module)
	index := analysis.NewIndex(scope.Files)
	index.Resolution = scope.Resolution
	if err := index.AddSources(ctx, scope.Names); err != nil {
		return nil, nil, err
	}
	groups := map[string]analysis.BuildScope{}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if index.EvidenceCount > evidenceLimit {
			return analysis.ErrEvidenceLimit
		}
		if len(index.Edges) > edgeLimit {
			return analysis.ErrEdgeLimit
		}
		return nil
	}
	adapters := map[string]analysis.Adapter{}
	var organizationEdges []analysis.Edge
	for _, name := range names {
		if err := check(); err != nil {
			return nil, nil, err
		}
		a := lookup(name)
		adapters[name] = a
		if a.Organizer == nil {
			continue
		}
		organization, err := a.Organizer.Organize(ctx, groups[name])
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
	if nodes > nodeLimit {
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
	var bindings []analysis.BindResult
	var issues []analysis.Gap
	for _, name := range names {
		if err := check(); err != nil {
			return nil, nil, err
		}
		b := adapters[name].Binder
		if b == nil {
			continue
		}
		bound, err := b.Bind(ctx, groups[name], index, evidenceLimit-index.EvidenceCount)
		if err != nil {
			return nil, nil, fmt.Errorf("bind %s: %w", name, err)
		}
		for _, e := range bound.Edges {
			index.Add(e)
		}
		issues = append(issues, bound.Issues...)
		bindings = append(bindings, bound)
	}
	for _, b := range bindings {
		if err := check(); err != nil {
			return nil, nil, err
		}
		if b.Resolver == nil {
			continue
		}
		edges, gaps, err := b.Resolver.Resolve(ctx, index, evidenceLimit-index.EvidenceCount)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range edges {
			index.Add(e)
		}
		issues = append(issues, gaps...)
	}
	if err := check(); err != nil {
		return nil, nil, err
	}
	return index, append(issues, index.Conflicts()...), nil
}

var ErrNodeLimit = fmt.Errorf("node limit reached")

func Builtins(ctx context.Context, files map[string]analysis.Facts, module string, nodes, edges, evidence int) (*analysis.Index, []analysis.Gap, error) {
	return Build(ctx, analysis.NewBuildScope(files, module), nodes, edges, evidence, language.Lookup)
}

func BuiltinsWithResolution(ctx context.Context, files map[string]analysis.Facts, module string, resolution analysis.ResolutionContext, nodes, edges, evidence int) (*analysis.Index, []analysis.Gap, error) {
	scope := analysis.NewBuildScope(files, module)
	scope.Resolution = resolution
	return Build(ctx, scope, nodes, edges, evidence, language.Lookup)
}
