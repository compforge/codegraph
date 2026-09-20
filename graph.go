// Package codegraph provides an embedded, source-aware property graph for Go programs.
package codegraph

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"sync"
	"time"

	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/graphstore"
)

var (
	ErrSnapshotChanged = errors.New("source changed within graph snapshot")
	ErrBuildBudget     = errors.New("build budget exceeded")
	ErrQueryBudget     = graphstore.ErrBudget
	ErrReadOnly        = graphstore.ErrReadOnly
)

// Options bounds a graph's build and query work. Zero values select finite defaults.
// Scope contains slash-separated file paths or directory prefixes relative to fs.FS.
// Empty Scope allows any relative path; files are only loaded when requested or expanded.
type Options struct {
	ModulePath                                 string
	Scope                                      []string
	ExpandImports                              bool
	MaxDepth, MaxFiles, MaxNodes, MaxRelations int
	MaxFileBytes, MaxSourceBytes               int64
	ParseTimeout, QueryTimeout                 time.Duration
	MaxQueryHops, MaxResultRows                int
	MaxResultBytes                             int64
}

// Graph owns one immutable source snapshot, extended through atomic build batches.
// Queries observe either the old or the new batch; they cannot mutate the graph.
// +spec=`A failed or cancelled build must not publish a partially written graph`
type Graph struct {
	mu        sync.RWMutex
	buildMu   sync.Mutex
	snapshot  string
	opts      Options
	files     map[string]extract.Facts
	failures  map[string]Diagnostic
	nodes     map[string]Node
	relations map[string]Relation
	store     *graphstore.Store
	report    BuildReport
}

func New(snapshot string, opts Options) (*Graph, error) {
	if snapshot == "" {
		return nil, errors.New("snapshot identity is required")
	}
	if err := defaults(&opts); err != nil {
		return nil, err
	}
	g := &Graph{snapshot: snapshot, opts: opts, files: map[string]extract.Facts{}, failures: map[string]Diagnostic{}, nodes: map[string]Node{}, relations: map[string]Relation{}}
	g.store = graphstore.New(g.limits())
	g.report = BuildReport{Snapshot: snapshot, Complete: true, Files: []string{}}
	return g, nil
}

func defaults(o *Options) error {
	for _, pair := range []struct {
		v   *int
		def int
	}{{&o.MaxDepth, 4}, {&o.MaxFiles, 256}, {&o.MaxNodes, 50000}, {&o.MaxRelations, 100000}, {&o.MaxQueryHops, 8}, {&o.MaxResultRows, 1000}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	for _, pair := range []struct {
		v   *int64
		def int64
	}{{&o.MaxFileBytes, 2 << 20}, {&o.MaxSourceBytes, 32 << 20}, {&o.MaxResultBytes, 8 << 20}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	if o.ParseTimeout < 0 || o.QueryTimeout < 0 {
		return errors.New("timeouts must not be negative")
	}
	if o.ParseTimeout == 0 {
		o.ParseTimeout = 2 * time.Second
	}
	if o.QueryTimeout == 0 {
		o.QueryTimeout = 5 * time.Second
	}
	o.Scope = append([]string(nil), o.Scope...)
	for _, p := range o.Scope {
		if !fs.ValidPath(p) {
			return fmt.Errorf("invalid scope %q", p)
		}
	}
	return nil
}

func (g *Graph) limits() graphstore.Limits {
	return graphstore.Limits{Rows: g.opts.MaxResultRows, Bytes: g.opts.MaxResultBytes, Hops: g.opts.MaxQueryHops}
}

func (g *Graph) Snapshot() string { return g.snapshot }

// Nodes returns independent values in source-ID order.
func (g *Graph) Nodes() []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, cloneNode(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Relations() []Relation {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Relation, 0, len(g.relations))
	for _, r := range g.relations {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Report() BuildReport {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return cloneReport(g.report)
}

// Capabilities describes implemented extraction/resolution, not grammar availability.
func Capabilities() []Capability {
	return []Capability{{Language: "go", Declarations: []NodeKind{Function, Method, Struct, Interface, Field, Type, TypeAlias}, Relations: []RelationKind{Contains, Imports, Calls}, Markers: []MarkerKind{Spec, Case, Rule, Link, Doc}, Limitations: []string{"static package functions only; receiver and callback dispatch remain unresolved", "members are extracted only from named struct/interface literals; anonymous nested types and promoted members are not expanded", "build tags and compiler type checking are not evaluated", "marker syntax: declaration comments using +kind=payload or +kind:payload"}}}
}

type Capability struct {
	Language     string
	Declarations []NodeKind
	Relations    []RelationKind
	Markers      []MarkerKind
	Limitations  []string
}
