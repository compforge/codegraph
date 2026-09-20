// Package codegraph provides an embedded, language-neutral code property graph.
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
	"github.com/odvcencio/gotreesitter/grammars"
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
// With no names it returns the language-specific adapters. Pass grammar names
// from Languages to inspect additional outline support without eagerly loading
// every registered grammar.
func Capabilities(languages ...string) []Capability {
	if len(languages) == 0 {
		languages = []string{"go", "python", "javascript", "typescript", "tsx"}
	}
	var out []Capability
	for _, name := range languages {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil {
			continue
		}
		if entry.Name == "go" {
			out = append(out, Capability{Language: "go", Declarations: []NodeKind{Function, Method, Struct, Interface, Field, Type, TypeAlias, Variable, Constant}, Relations: []RelationKind{Contains, Imports, Calls}, Markers: []MarkerKind{Spec, Case, Rule, Link, Doc}, Limitations: []string{"static package functions only; receiver and callback dispatch remain unresolved", "variables and constants require a single declared name", "members are extracted only from named struct/interface literals; anonymous nested types and promoted members are not expanded", "build tags and compiler type checking are not evaluated", "marker syntax: declaration comments using +kind=payload or +kind:payload"}})
			continue
		}
		cap := Capability{Language: entry.Name, Relations: []RelationKind{Contains}, Limitations: []string{"outline is limited to grammar tags; runtime omissions are diagnostics"}}
		for _, kind := range extract.DeclarationKinds(*entry) {
			cap.Declarations = append(cap.Declarations, NodeKind(kind))
		}
		if extract.ModuleLanguage(entry.Name) {
			cap.Relations = append(cap.Relations, Imports, Calls)
			cap.Markers = []MarkerKind{Spec, Case, Rule, Link, Doc}
			cap.Limitations = append(cap.Limitations, "calls resolve only to unshadowed module-level functions in the same file; imported calls and dynamic dispatch remain unresolved", "imports use local source paths only; dependency configuration, exports, runtime paths and third-party modules are not evaluated; Python absolute imports are candidates")
		} else {
			cap.Limitations = append(cap.Limitations, "syntax/outline fallback only; reference resolution and markers are not implemented; builds report partial coverage")
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
	if entry := extract.Detect(name); entry != nil {
		return entry.Name
	}
	return ""
}

type Capability struct {
	Language     string
	Declarations []NodeKind
	Relations    []RelationKind
	Markers      []MarkerKind
	Limitations  []string
}
