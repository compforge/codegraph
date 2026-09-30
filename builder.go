package codegraph

import (
	"context"
	"sync"

	"github.com/compforge/codegraph/internal/build"
)

// Options bounds a graph's build and query work. Zero values select finite defaults.
// Scope contains snapshot-relative, slash-separated document paths or directory prefixes.
// Empty Scope allows any relative path; documents are only analyzed when supplied.
type Options = build.Options

// ResolutionContext supplies snapshot-specific repository knowledge. It never
// loads files: only supplied Facts can become graph endpoints. Omitted imports
// use language defaults; an explicit empty Targets list means known unresolved.
type ResolutionContext = build.ResolutionContext

// ImportResolution identifies a lexical import occurrence and supplies its
// candidate module documents. Targets exclude merely incidental dependencies
// (for example Python ancestor initializers). Confidence caps every derived
// binding; it is required even when no target exists.
type ImportResolution = build.ImportResolution

// Builder admits reusable Facts and publishes immutable Graph results.
// Compatibility document scheduling is created only when its API is used.
type Builder struct {
	core        *build.Builder
	mu          sync.Mutex
	last        *Graph
	sessionOnce sync.Once
	session     *build.Session
	sessionErr  error
}

func NewBuilder(snapshot string, opts Options) (*Builder, error) {
	core, err := build.NewBuilder(snapshot, opts)
	if err != nil {
		return nil, err
	}
	return &Builder{core: core}, nil
}

// Add atomically admits extracted material. No parser or query engine runs here.
// Identical versions are idempotent; changing a path's content requires a new builder.
func (b *Builder) Add(facts ...Facts) error { return b.core.Add(facts...) }

// AddFailure retains a document-local extraction gap without inventing facts.
// The caller must not pass context cancellation as a local parse failure.
func (b *Builder) AddFailure(doc Document, failure error) error {
	return b.core.AddFailure(doc, failure)
}

// SetResolutionContext replaces repository knowledge before the next Build.
// It copies all input collections; previously published graphs remain unchanged.
func (b *Builder) SetResolutionContext(context ResolutionContext) error {
	return b.core.SetResolutionContext(context)
}

// Build runs Organize, Bind and Resolve using only admitted facts. A caller can
// keep older results while adding more facts and publishing another result.
func (b *Builder) Build(ctx context.Context) (*Graph, BuildReport, error) {
	snapshot, report, err := b.core.Build(ctx)
	if err != nil {
		return nil, report, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last != nil && b.last.view == snapshot {
		return b.last, report, nil
	}
	graph := &Graph{view: snapshot}
	// Another build can finish before this wrapper is created. Return this
	// publication without replacing the wrapper for a newer Result.
	if b.core.Result() == snapshot {
		b.last = graph
	}
	return graph, report, nil
}

// Result is the last successful publication (initially an empty graph).
func (b *Builder) Result() *Graph {
	b.mu.Lock()
	defer b.mu.Unlock()
	snapshot := b.core.Result()
	if b.last == nil || b.last.view != snapshot {
		b.last = &Graph{view: snapshot}
	}
	return b.last
}
func (b *Builder) Report() BuildReport { return b.core.Report() }
func (b *Builder) legacySession() (*build.Session, error) {
	b.sessionOnce.Do(func() { b.session, b.sessionErr = build.NewSession(b.core) })
	return b.session, b.sessionErr
}
