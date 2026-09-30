package codegraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/compforge/codegraph/internal/pipeline"
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
		return fmt.Errorf("%w: file %s exceeds byte limit", ErrBuildBudget, doc.Path)
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
		return completedTask[Facts]{Err: err}
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

// ExtractionCache reuses detached single-document facts across Graphs. It never
// retains bound relationships or graph state. The caller owns its lifetime;
// keep the grammar registry unchanged while sharing it. It is safe for concurrent
// Graphs, but concurrent misses may extract independently.
// Capacity bounds retained document versions and their source bytes, not total
// heap usage. A full cache skips new entries without changing build behavior.
type ExtractionCache struct {
	mu                          sync.Mutex
	entries                     map[extractionKey]analysis.Facts
	maxDocuments                int
	maxSourceBytes, sourceBytes int64
}

type extractionKey struct {
	path   string
	digest [32]byte
}

// NewExtractionCache creates a bounded, in-memory extraction cache. Zero limits
// select 256 document versions and 32 MiB of source; negative limits are invalid.
func NewExtractionCache(maxDocuments int, maxSourceBytes int64) (*ExtractionCache, error) {
	if maxDocuments < 0 || maxSourceBytes < 0 {
		return nil, errors.New("cache limits must not be negative")
	}
	if maxDocuments == 0 {
		maxDocuments = 256
	}
	if maxSourceBytes == 0 {
		maxSourceBytes = 32 << 20
	}
	return &ExtractionCache{entries: map[extractionKey]analysis.Facts{}, maxDocuments: maxDocuments, maxSourceBytes: maxSourceBytes}, nil
}

func (c *ExtractionCache) get(key extractionKey) (analysis.Facts, bool) {
	if c == nil {
		return analysis.Facts{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	facts, ok := c.entries[key]
	return facts, ok
}

func (c *ExtractionCache) put(key extractionKey, facts analysis.Facts) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	size := int64(len(facts.Source) + len(facts.Gitlink))
	if len(c.entries) >= c.maxDocuments || size > c.maxSourceBytes-c.sourceBytes {
		return
	}
	// Raw facts are immutable after extraction. Each Graph creates its own
	// binding index, and the public Facts projection returns independent values.
	c.entries[key] = facts
	c.sourceBytes += size
}

// extractMaterial owns classification and raw extraction only. Callers own
// admission, scheduling, and atomic publication.
func (g *Extractor) extractMaterial(ctx context.Context, document Document) (analysis.Facts, error) {
	if err := ctx.Err(); err != nil {
		return analysis.Facts{}, err
	}
	var key extractionKey
	if g.opts.Cache != nil {
		key = extractionKey{path: document.Path, digest: document.digest()}
		if facts, ok := g.opts.Cache.get(key); ok {
			return facts, ctx.Err()
		}
	}
	var facts analysis.Facts
	if document.Gitlink != "" {
		facts = pipeline.DocumentOnly(document.Path, nil)
		facts.Gitlink = document.Gitlink
	} else if language.Detect(document.Path) == nil {
		facts = pipeline.DocumentOnly(document.Path, bytes.Clone(document.Content))
		facts.Issues = append(facts.Issues, analysis.Issue{Code: "unsupported_language", Message: "no registered grammar for file", Subject: "document", Span: analysis.Span{End: len(document.Content)}})
	} else {
		if parseObserver != nil {
			parseObserver(document.Path)
		}
		var err error
		facts, err = pipeline.Analyze(ctx, document.Path, bytes.Clone(document.Content), g.opts.ParseTimeout)
		if err != nil {
			return analysis.Facts{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return analysis.Facts{}, err
	}
	g.opts.Cache.put(key, facts)
	return facts, nil
}

type completedTask[T any] struct {
	Value T
	Err   error
}

var completed = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

func (t completedTask[T]) Done() <-chan struct{} { return completed }

func (t completedTask[T]) Wait() (T, error) { return t.Value, t.Err }

// parseObserver is test instrumentation; set only while no unrelated parser runs.
var parseObserver func(string)
