package build

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/graphstore"
	"github.com/compforge/codegraph/internal/model"
	"github.com/compforge/codegraph/internal/pipeline"
)

// Builder collects complete Facts for one source snapshot and publishes a new
// immutable Graph on each successful Build. Failed builds leave Result intact.
// Snapshot-specific binding is never cached in Facts or Extractor.
type Builder struct {
	resolution  analysis.ResolutionContext
	mu          sync.RWMutex
	buildMu     sync.Mutex
	snapshot    string
	opts        Options
	documents   map[string]analysis.Facts
	sourceBytes int64
	failures    map[string]model.Diagnostic
	result      *graphstore.Snapshot
}

func NewBuilder(snapshot string, opts Options) (*Builder, error) {
	if snapshot == "" {
		return nil, errors.New("snapshot identity is required")
	}
	if err := defaults(&opts); err != nil {
		return nil, err
	}
	resolution, err := compileResolution(opts.ResolutionContext, opts.MaxEvidence)
	if err != nil {
		return nil, err
	}
	opts.ResolutionContext = ResolutionContext{}
	b := &Builder{snapshot: snapshot, opts: opts, documents: map[string]analysis.Facts{}, failures: map[string]model.Diagnostic{}}
	b.resolution = resolution
	b.result = newGraph(snapshot, opts, map[string]model.Node{}, map[string]model.Relation{}, model.BuildReport{Snapshot: snapshot, Documents: []string{}})
	return b, nil
}

// Add atomically admits extracted material. No parser or query engine runs here.
// Identical versions are idempotent; changing a path's content requires a new builder.
func (b *Builder) Add(facts ...extract.Facts) error {
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	staged := make(map[string]analysis.Facts, len(facts))
	total := b.sourceBytes
	for _, f := range facts {
		raw, valid := extract.Material(f)
		if !valid {
			return fmt.Errorf("facts must be produced by an Extractor")
		}
		doc := extract.Document{Path: raw.Path, Content: raw.Source, Gitlink: raw.Gitlink}
		if !b.allowed(raw.Path) {
			return fmt.Errorf("facts %s are outside allowed scope", raw.Path)
		}
		old, ok := staged[raw.Path]
		if !ok {
			old, ok = b.documents[raw.Path]
		}
		if ok {
			if !extract.Matches(doc, old) {
				return fmt.Errorf("%w: %s", model.ErrSnapshotChanged, raw.Path)
			}
			continue
		}
		if extract.Size(doc) > b.opts.MaxDocumentBytes || len(b.documents)+len(staged) >= b.opts.MaxDocuments || extract.Size(doc) > b.opts.MaxSourceBytes-total {
			return fmt.Errorf("%w: %s", model.ErrBuildBudget, raw.Path)
		}
		staged[raw.Path] = raw
		total += extract.Size(doc)
	}
	// A regular source only needs its ancestors checked. Opaque gitlinks also
	// check descendants. This keeps ordinary one-at-a-time admission linear in
	// path depth rather than copying and sorting the growing snapshot each time.
	for p, f := range staged {
		for parent := path.Dir(p); ; parent = path.Dir(parent) {
			if b.documents[parent].Gitlink != "" || staged[parent].Gitlink != "" {
				return fmt.Errorf("document %s is inside opaque gitlink %s", p, parent)
			}
			if parent == "." {
				break
			}
		}
		if f.Gitlink == "" {
			continue
		}
		for name := range b.documents {
			if strings.HasPrefix(name, p+"/") {
				return fmt.Errorf("gitlink %s contains document %s", p, name)
			}
		}
		for name := range staged {
			if strings.HasPrefix(name, p+"/") {
				return fmt.Errorf("gitlink %s contains document %s", p, name)
			}
		}
	}
	for p, f := range staged {
		b.documents[p] = f
	}
	b.sourceBytes = total
	for _, f := range facts {
		raw, _ := extract.Material(f)
		delete(b.failures, raw.Path)
	}
	return nil
}

// AddFailure retains a document-local extraction gap without inventing facts.
// The caller must not pass context cancellation as a local parse failure.
func (b *Builder) AddFailure(doc extract.Document, failure error) error {
	if err := extract.Validate(doc); err != nil {
		return err
	}
	if failure == nil {
		return errors.New("extraction failure is required")
	}
	if errors.Is(failure, model.ErrBuildBudget) || errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
		return failure
	}
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.documents[doc.Path]; ok {
		return fmt.Errorf("facts already admitted for %s", doc.Path)
	}
	if !b.allowed(doc.Path) {
		return fmt.Errorf("document %s is outside allowed scope", doc.Path)
	}
	if extract.Size(doc) > b.opts.MaxDocumentBytes {
		return fmt.Errorf("%w: %s", model.ErrBuildBudget, doc.Path)
	}
	b.failures[doc.Path] = model.Diagnostic{Code: "parse_error", Message: failure.Error(), Subject: model.DocumentSubject, Location: model.SourceLocation(pipeline.DocumentOnly(doc.Path, doc.Content), analysis.Span{End: len(doc.Content)})}
	return nil
}

// Build runs Organize, Bind and Resolve using only admitted facts. A caller can
// keep older results while adding more facts and publishing another result.
func (b *Builder) Build(ctx context.Context) (*graphstore.Snapshot, model.BuildReport, error) {
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, b.Report(), err
	}
	nodes, relations, report, err := b.assemble(ctx, b.documents, b.failures)
	if err != nil {
		return nil, b.Report(), err
	}
	if err := ctx.Err(); err != nil {
		return nil, b.Report(), err
	}
	result := newGraph(b.snapshot, b.opts, nodes, relations, report)
	b.mu.Lock()
	b.result = result
	b.mu.Unlock()
	return result, model.CloneReport(report), nil
}

// Result is the last successful publication (initially an empty graph).
func (b *Builder) Result() *graphstore.Snapshot { b.mu.RLock(); defer b.mu.RUnlock(); return b.result }
func (b *Builder) Report() model.BuildReport    { return b.Result().Report() }
