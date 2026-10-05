package codegraph

import (
	"context"
	"sort"
)

type namespaceStep struct {
	parent   string
	relation string
}

type namespaceIndex struct {
	parents map[string][]namespaceStep
	owners  map[string]bool
}

// Each immutable publication owns its index. Failed or canceled construction is
// not cached; relation evidence remains in the graph, independent of query policy.
func (g *Graph) namespaces(ctx context.Context) (*namespaceIndex, error) {
	g.namespaceMu.Lock()
	defer g.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.namespaceIndex != nil {
		return g.namespaceIndex, nil
	}
	index := &namespaceIndex{parents: map[string][]namespaceStep{}, owners: map[string]bool{}}
	for id, n := range g.nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if namespaceKind(n.Kind) {
			index.owners[id] = true
		}
	}
	for id, r := range g.relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch r.Kind {
		case Contains:
			index.parents[r.Target] = append(index.parents[r.Target], namespaceStep{r.Source, id})
			index.owners[r.Source] = true
		case InNamespace:
			index.parents[r.Source] = append(index.parents[r.Source], namespaceStep{r.Target, id})
			index.owners[r.Target] = true
		case OccursIn:
			index.parents[r.Source] = append(index.parents[r.Source], namespaceStep{r.Target, id})
		}
	}
	for _, parents := range index.parents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sort.Slice(parents, func(i, j int) bool { return parents[i].relation < parents[j].relation })
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.namespaceIndex = index
	return index, nil
}

// Concrete kinds cover empty namespaces; graph ownership also admits other
// language-defined member owners, such as a function containing local declarations.
func namespaceKind(kind NodeKind) bool {
	switch kind {
	case Package, Module, Namespace, Class, Struct, Interface, Type, Enum, Record, Trait, Union:
		return true
	}
	return false
}
