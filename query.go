package codegraph

import (
	"context"
	"fmt"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Query returns detached Go values: Node, Relation, Path, scalar values,
// []any and map[string]any. Integer scalars are int64. No partial rows are
// returned when execution fails. Variable paths require explicit upper bounds.
// +rule=`Query entities must use source identities; Cypher id(n) is opaque and not portable between batches`
func (g *Graph) Query(ctx context.Context, q string, params map[string]any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, g.opts.QueryTimeout)
	defer cancel()
	g.mu.RLock()
	defer g.mu.RUnlock()
	rows, err := g.store.Query(ctx, q, params)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		for k, v := range row {
			x, err := g.project(v)
			if err != nil {
				return nil, err
			}
			row[k] = x
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func (g *Graph) project(v any) (any, error) {
	switch v := v.(type) {
	case graphstore.Entity:
		if v.Relation {
			r, ok := g.relations[v.ID]
			if !ok {
				return nil, fmt.Errorf("unknown relation %s", v.ID)
			}
			return r, nil
		}
		n, ok := g.nodes[v.ID]
		if !ok {
			return nil, fmt.Errorf("unknown node %s", v.ID)
		}
		return cloneNode(n), nil
	case graphstore.Path:
		p := Path{}
		for _, n := range v.Nodes {
			x, err := g.project(n)
			if err != nil {
				return nil, err
			}
			p.Nodes = append(p.Nodes, x.(Node))
		}
		for _, r := range v.Relations {
			x, err := g.project(r)
			if err != nil {
				return nil, err
			}
			p.Relations = append(p.Relations, x.(Relation))
		}
		return p, nil
	case []any:
		for i, x := range v {
			p, err := g.project(x)
			if err != nil {
				return nil, err
			}
			v[i] = p
		}
		return v, nil
	case map[string]any:
		for k, x := range v {
			p, err := g.project(x)
			if err != nil {
				return nil, err
			}
			v[k] = p
		}
		return v, nil
	default:
		return v, nil
	}
}
