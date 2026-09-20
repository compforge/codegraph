// Package graphstore owns the GoGraph boundary; its public-to-parent types are
// detached references and Go values, never syntax or graph-engine objects.
package graphstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/graph/adjlist"
	"github.com/FlavioCFOliveira/GoGraph/graph/lpg"
)

var ErrBudget = errors.New("query budget exceeded")
var ErrReadOnly = errors.New("query must be read-only")

type Entity struct {
	ID       string
	Relation bool
}
type Path struct{ Nodes, Relations []Entity }
type Limits struct {
	Rows  int
	Bytes int64
	Hops  int
}
type Store struct {
	graph       *lpg.Graph[string, float64]
	engine      *cypher.Engine
	limits      Limits
	relationIDs map[uint64]string
}

func New(limits Limits) *Store {
	g := lpg.New[string, float64](adjlist.Config{Directed: true, Multigraph: true, Weightless: true})
	return &Store{graph: g, engine: cypher.NewEngineWithOptions(g, cypher.EngineOptions{MaxResultRows: int64(limits.Rows), MaxResultBytes: limits.Bytes, MaxCollectItems: limits.Rows, GlobalMaxResultBytes: limits.Bytes}), limits: limits, relationIDs: map[uint64]string{}}
}

func (s *Store) AddNode(id, label string, props map[string]any) error {
	if err := s.graph.AddNode(id); err != nil {
		return err
	}
	if err := s.graph.SetNodeLabel(id, label); err != nil {
		return err
	}
	for k, v := range props {
		pv, err := property(v)
		if err != nil {
			return err
		}
		if err = s.graph.SetNodeProperty(id, k, pv); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AddEdge(source, target, kind string, props map[string]any) error {
	id, ok := props["id"].(string)
	if !ok {
		return errors.New("edge requires source identity")
	}
	h, err := s.graph.AddEdgeH(source, target, 0)
	if err != nil {
		return err
	}
	s.graph.SetEdgeLabelByHandle(source, target, h, kind)
	s.relationIDs[h] = id
	for k, v := range props {
		pv, err := property(v)
		if err != nil {
			return err
		}
		if err = s.graph.SetEdgePropertyByHandle(source, target, h, k, pv); err != nil {
			return err
		}
	}
	return nil
}

func property(v any) (lpg.PropertyValue, error) {
	switch v := v.(type) {
	case string:
		return lpg.StringValue(v), nil
	case int:
		return lpg.Int64Value(int64(v)), nil
	case []string:
		items := make([]lpg.PropertyValue, len(v))
		for i, x := range v {
			items[i] = lpg.StringValue(x)
		}
		return lpg.ListValue(items), nil
	default:
		return lpg.PropertyValue{}, fmt.Errorf("unsupported property type %T", v)
	}
}

func (s *Store) Query(ctx context.Context, q string, params map[string]any) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkQuery(q, s.limits.Hops); err != nil {
		return nil, err
	}
	bound, err := cypher.BindParams(params)
	if err != nil {
		return nil, err
	}
	tx, err := s.engine.BeginReadTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(q, bound)
	if err != nil {
		return nil, queryError(err)
	}
	defer res.Close()
	rows := []map[string]any{}
	for res.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for k, v := range res.Record() {
			value, err := s.detach(v)
			if err != nil {
				return nil, err
			}
			row[k] = value
		}
		rows = append(rows, row)
	}
	if err := res.Err(); err != nil {
		return nil, queryError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func queryError(err error) error {
	if errors.Is(err, cypher.ErrWriteInReadOnlyTx) {
		return fmt.Errorf("%w: %v", ErrReadOnly, err)
	}
	if errors.Is(err, cypher.ErrResultRowsExceeded) || errors.Is(err, cypher.ErrResultBytesExceeded) {
		return fmt.Errorf("%w: %v", ErrBudget, err)
	}
	return err
}

func entity(props expr.MapValue, relation bool) (Entity, error) {
	id, ok := props["id"].(expr.StringValue)
	if !ok {
		return Entity{}, errors.New("query entity has no source identity")
	}
	return Entity{string(id), relation}, nil
}

func (s *Store) relationship(handle uint64) (Entity, error) {
	// Entity identity is independent of property materialization. GoGraph's
	// chained RETURN r fast path can omit by-handle properties; its stable
	// relationship handle still identifies the exact parallel edge.
	id, ok := s.relationIDs[handle]
	if !ok {
		return Entity{}, fmt.Errorf("unknown edge handle %d", handle)
	}
	return Entity{ID: id, Relation: true}, nil
}

func (s *Store) detach(v any) (any, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case expr.NodeValue:
		return entity(v.Properties, false)
	case expr.RelationshipValue:
		return s.relationship(v.ID)
	case *expr.LazyNodeValue:
		return entity(expr.MapValue{"id": v.Property("id")}, false)
	case *expr.LazyRelationshipValue:
		return s.relationship(v.ID())
	case expr.PathValue:
		p := Path{}
		for _, n := range v.Nodes {
			e, err := entity(n.Properties, false)
			if err != nil {
				return nil, err
			}
			p.Nodes = append(p.Nodes, e)
		}
		for _, r := range v.Relationships {
			e, err := s.relationship(r.ID)
			if err != nil {
				return nil, err
			}
			p.Relations = append(p.Relations, e)
		}
		return p, nil
	case expr.StringValue:
		return string(v), nil
	case expr.IntegerValue:
		return int64(v), nil
	case expr.FloatValue:
		return float64(v), nil
	case expr.BoolValue:
		return bool(v), nil
	case expr.ListValue:
		out := make([]any, len(v))
		for i, x := range v {
			d, err := s.detach(x)
			if err != nil {
				return nil, err
			}
			out[i] = d
		}
		return out, nil
	case expr.MapValue:
		out := map[string]any{}
		for k, x := range v {
			d, err := s.detach(x)
			if err != nil {
				return nil, err
			}
			out[k] = d
		}
		return out, nil
	case expr.Value:
		if expr.IsNull(v) {
			return nil, nil
		}
		return nil, fmt.Errorf("unsupported query result kind %v", v.Kind())
	default:
		return nil, fmt.Errorf("unsupported query result type %T", v)
	}
}
