package graphstore

import (
	"sort"
	"sync"
	"time"

	"github.com/compforge/codegraph/internal/model"
)

// Snapshot is an immutable publication with a lazily materialized query index.
// It owns no extraction, admission or compatibility state.
type Snapshot struct {
	snapshot  string
	nodes     map[string]model.Node
	relations map[string]model.Relation
	report    model.BuildReport
	limits    Limits
	timeout   time.Duration
	storeMu   sync.Mutex
	store     *Store
}

// NewSnapshot takes ownership of nodes, relations and report. Callers must not
// mutate them after publication; read methods return detached values.
func NewSnapshot(snapshot string, nodes map[string]model.Node, relations map[string]model.Relation, report model.BuildReport, limits Limits, timeout time.Duration) *Snapshot {
	return &Snapshot{snapshot: snapshot, nodes: nodes, relations: relations, report: report, limits: limits, timeout: timeout}
}
func (g *Snapshot) Snapshot() string { return g.snapshot }

// Nodes returns independent values in source-ID order.
func (g *Snapshot) Nodes() []model.Node {
	out := make([]model.Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, model.CloneNode(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Snapshot) Relations() []model.Relation {
	out := make([]model.Relation, 0, len(g.relations))
	for _, r := range g.relations {
		out = append(out, model.CloneRelation(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Snapshot) Report() model.BuildReport {
	return model.CloneReport(g.report)
}
