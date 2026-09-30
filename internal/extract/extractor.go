package extract

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/graphmodel"
)

// ExtractionOptions controls parser work independently of any graph snapshot.
// Cache is optional and caller-owned; a Facts value remains usable after eviction.
type ExtractionOptions struct {
	Concurrency      int
	MaxDocumentBytes int64
	ParseTimeout     time.Duration
	Cache            *ExtractionCache
}

// Extractor owns bounded parsing. It has no graph, binding or snapshot state.
// Extract and Submit may be called concurrently. No worker survives its task.
type Extractor struct {
	opts  ExtractionOptions
	slots chan struct{}
}

func NewExtractor(opts ExtractionOptions) (*Extractor, error) {
	if opts.Concurrency < 0 || opts.MaxDocumentBytes < 0 || opts.ParseTimeout < 0 {
		return nil, fmt.Errorf("extraction limits must not be negative")
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = min(runtime.GOMAXPROCS(0), 4)
	}
	if opts.MaxDocumentBytes == 0 {
		opts.MaxDocumentBytes = 2 << 20
	}
	if opts.ParseTimeout == 0 {
		opts.ParseTimeout = 2 * time.Second
	}
	return &Extractor{opts: opts, slots: make(chan struct{}, opts.Concurrency)}, nil
}

func (e *Extractor) acquire(ctx context.Context, doc Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := doc.validate(); err != nil {
		return err
	}
	if doc.size() > e.opts.MaxDocumentBytes {
		return fmt.Errorf("%w: file %s exceeds byte limit", graphmodel.ErrBuildBudget, doc.Path)
	}
	select {
	case e.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Extractor) extract(ctx context.Context, doc Document) (analysis.Facts, error) {
	if err := e.acquire(ctx, doc); err != nil {
		return analysis.Facts{}, err
	}
	defer func() { <-e.slots }()
	return e.extractMaterial(ctx, doc)
}

func (e *Extractor) Extract(ctx context.Context, doc Document) (Facts, error) {
	raw, err := e.extract(ctx, doc)
	if err != nil {
		return Facts{}, err
	}
	return projectFacts(raw)
}

type extractionTask struct {
	done chan struct{}
	raw  analysis.Facts
	err  error
}

func (t *extractionTask) Done() <-chan struct{} { return t.done }
func (t *extractionTask) Wait() (Facts, error) {
	<-t.done
	if t.err != nil {
		return Facts{}, t.err
	}
	return projectFacts(t.raw)
}

// Submit copies admitted input bytes and starts extraction. Admission blocks
// when all parser slots are occupied; cancellation releases a blocked submitter.
// Wait joins the parser and returns a fresh view on every call.
func (e *Extractor) Submit(ctx context.Context, doc Document) pond.ResultTask[Facts] {
	if err := e.acquire(ctx, doc); err != nil {
		return Completed[Facts]{Err: err}
	}
	doc.Content = bytes.Clone(doc.Content)
	task := &extractionTask{done: make(chan struct{})}
	go func() {
		defer close(task.done)
		defer func() { <-e.slots }()
		task.raw, task.err = e.extractMaterial(ctx, doc)
	}()
	return task
}
