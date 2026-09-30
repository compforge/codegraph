package codegraph

import (
	"context"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Graph is a read-only publication. New and package-level Build preserve the
// incremental document API through a separate Builder compatibility session.
type Graph struct {
	view   *graphstore.Snapshot
	legacy *Builder
}

// New creates the compatibility incremental facade. Prefer NewBuilder when
// extraction and snapshot construction have separate lifetimes.
func New(snapshot string, opts Options) (*Graph, error) {
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, err
	}
	return &Graph{legacy: b}, nil
}
func (g *Graph) current() *graphstore.Snapshot {
	if g.legacy != nil {
		return g.legacy.core.Result()
	}
	return g.view
}
func (g *Graph) Snapshot() string { return g.current().Snapshot() }

// Nodes returns independent values in source-ID order.
func (g *Graph) Nodes() []Node         { return g.current().Nodes() }
func (g *Graph) Relations() []Relation { return g.current().Relations() }
func (g *Graph) Report() BuildReport   { return g.current().Report() }

// Node returns a detached node by its source identity.
func (g *Graph) Node(id string) (Node, bool) { return g.current().Node(id) }

// Find returns declaration nodes in source order. An empty kind matches every
// declaration kind; an empty qualifiedName matches every name. Documents and
// organizations without a single Location are excluded. Follow declares from a
// Document to find its package/module contributions.
// The returned nodes are detached values and can be safely modified.
func (g *Graph) Find(path string, kind NodeKind, qualifiedName string) []Node {
	return g.current().Find(path, kind, qualifiedName)
}

// RelationsFrom and RelationsTo expose bounded adjacency without requiring a
// consumer to parse Cypher. If kinds is empty, all relation kinds are returned.
func (g *Graph) RelationsFrom(id string, kinds ...RelationKind) []Relation {
	return g.current().RelationsFrom(id, kinds...)
}
func (g *Graph) RelationsTo(id string, kinds ...RelationKind) []Relation {
	return g.current().RelationsTo(id, kinds...)
}

// Query returns detached Go values: Node, Relation, Path, scalar values,
// []any and map[string]any. Integer scalars are int64. No partial rows are
// returned when execution fails. Variable paths require explicit upper bounds.
// +rule=`Query entities must use source identities; Cypher id(n) is opaque and not portable between batches`
func (g *Graph) Query(ctx context.Context, q string, params map[string]any) ([]map[string]any, error) {
	return g.current().Query(ctx, q, params)
}
