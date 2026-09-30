// Package codegraph provides an embedded, language-neutral code property graph.
package codegraph

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/compforge/codegraph/internal/language"

	"github.com/compforge/codegraph/internal/graphstore"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Options bounds a graph's build and query work. Zero values select finite defaults.
// Scope contains snapshot-relative, slash-separated document paths or directory prefixes.
// Empty Scope allows any relative path; documents are only analyzed when supplied.
type Options struct {
	ResolutionContext ResolutionContext
	// ExtractionCache optionally shares raw facts across snapshots; graph budgets
	// and relationship binding still apply independently to each Graph.
	ExtractionCache *ExtractionCache
	// BuildConcurrency bounds parallel document extraction within a batch.
	// Zero selects min(GOMAXPROCS, 4); one extracts serially.
	BuildConcurrency                     int
	ModulePath                           string
	Scope                                []string
	MaxDocuments, MaxNodes, MaxRelations int
	MaxEvidence                          int
	MaxDocumentBytes, MaxSourceBytes     int64
	ParseTimeout, QueryTimeout           time.Duration
	MaxQueryHops, MaxResultRows          int
	MaxResultBytes                       int64
}

// Graph is a read-only, successfully built snapshot. Its query index is lazy.
// Graphs returned by Builder.Build never change. New and the package-level Build
// retain the legacy incremental API through a separate Builder.
type Graph struct {
	snapshot  string
	opts      Options
	nodes     map[string]Node
	relations map[string]Relation
	report    BuildReport
	storeMu   sync.Mutex
	store     *graphstore.Store
	legacy    *Builder
}

// New creates the compatibility incremental facade. Prefer NewBuilder when
// extraction and snapshot construction have separate lifetimes.
func New(snapshot string, opts Options) (*Graph, error) {
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, err
	}
	return &Graph{snapshot: snapshot, opts: b.opts, legacy: b}, nil
}

func newGraph(snapshot string, opts Options, nodes map[string]Node, relations map[string]Relation, report BuildReport) *Graph {
	opts.ExtractionCache = nil
	opts.ResolutionContext = ResolutionContext{}
	opts.Scope = nil
	return &Graph{snapshot: snapshot, opts: opts, nodes: nodes, relations: relations, report: report}
}

func (g *Graph) current() *Graph {
	if g.legacy != nil {
		return g.legacy.Result()
	}
	return g
}

func defaults(o *Options) error {
	for _, pair := range []struct {
		v   *int
		def int
	}{{&o.BuildConcurrency, min(runtime.GOMAXPROCS(0), 4)}, {&o.MaxDocuments, 256}, {&o.MaxNodes, 50000}, {&o.MaxRelations, 100000}, {&o.MaxEvidence, 1000000}, {&o.MaxQueryHops, 8}, {&o.MaxResultRows, 1000}} {
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
	}{{&o.MaxDocumentBytes, 2 << 20}, {&o.MaxSourceBytes, 32 << 20}, {&o.MaxResultBytes, 8 << 20}} {
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
	g = g.current()
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, cloneNode(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Relations() []Relation {
	g = g.current()
	out := make([]Relation, 0, len(g.relations))
	for _, r := range g.relations {
		out = append(out, cloneRelation(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Report() BuildReport {
	g = g.current()
	return cloneReport(g.report)
}

// Capabilities describes implemented extraction/resolution, not grammar availability.
// With no names it returns the language-specific adapters. Pass grammar names
// from Languages to inspect additional outline support without eagerly loading
// every registered grammar.
func Capabilities(languages ...string) []Capability {
	if len(languages) == 0 {
		languages = language.Registered()
	}
	var out []Capability
	for _, name := range languages {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil {
			continue
		}
		c := language.Lookup(entry.Name).Describe(*entry)
		cap := Capability{Language: c.Language, Limitations: c.Limitations}
		for _, v := range c.Organizations {
			cap.Organizations = append(cap.Organizations, NodeKind(v))
		}
		for _, v := range c.Declarations {
			cap.Declarations = append(cap.Declarations, NodeKind(v))
		}
		for _, v := range c.Relations {
			cap.Relations = append(cap.Relations, RelationKind(v))
		}
		for _, v := range c.Markers {
			cap.Markers = append(cap.Markers, MarkerKind(v))
		}
		out = append(out, cap)
	}
	return out
}

// Languages lists registered grammar names without loading their parsers.
// Availability is not a guarantee of extraction or semantic completeness.
func Languages() []string {
	entries := grammars.AllLanguages()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// Language returns the registered grammar name selected for a source path, or
// an empty string. Recognition does not imply complete semantic coverage.
func Language(name string) string {
	if entry := language.Detect(name); entry != nil {
		return entry.Name
	}
	return ""
}

type Capability struct {
	// Organizations lists language units assembled from source contributions.
	Organizations []NodeKind
	Language      string
	Declarations  []NodeKind
	Relations     []RelationKind
	Markers       []MarkerKind
	Limitations   []string
}
