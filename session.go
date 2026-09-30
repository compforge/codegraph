package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/alitto/pond/v2"
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/pipeline"
)

// session coordinates the legacy document submission API. Plain builders never
// allocate its parser, task pool, pending batches or exploration cache.
type session struct {
	*Builder

	extractor      *Extractor
	asyncMu        sync.Mutex
	asyncPool      pond.ResultPool[Facts]
	building       bool
	pending        []Document
	pendingWorkers *sync.WaitGroup
	pendingWorks   []*buildWork
	latestWork     *buildWork
	documentTasks  map[string]documentTask
	factCacheMu    sync.Mutex
	factCache      map[string]factCacheEntry
}

func newSession(b *Builder) (*session, error) {
	e, err := NewExtractor(ExtractionOptions{Concurrency: b.opts.BuildConcurrency, MaxDocumentBytes: b.opts.MaxDocumentBytes, ParseTimeout: b.opts.ParseTimeout, Cache: b.opts.ExtractionCache})
	if err != nil {
		return nil, err
	}
	return &session{Builder: b, extractor: e, documentTasks: map[string]documentTask{}, factCache: map[string]factCacheEntry{}}, nil
}

// documentTask retains the submitted bytes so repeated paths cannot silently
// change the source snapshot while their extraction is in flight.
type documentTask struct {
	document Document
	ctx      context.Context
	task     pond.ResultTask[Facts]
	failed   bool
}

// buildWork is a completion barrier for one admission. A coordinator may
// combine several admissions into one atomic graph publication.
type buildWork struct {
	done   chan struct{}
	report BuildReport
	err    error
}

type mappedTask[A, B any] struct {
	source  pond.ResultTask[A]
	project func(A) (B, error)
}

func (t mappedTask[A, B]) Done() <-chan struct{} { return t.source.Done() }

func (t mappedTask[A, B]) Wait() (B, error) {
	value, err := t.source.Wait()
	if err != nil {
		var zero B
		return zero, err
	}
	return t.project(value)
}

// AddDocument queues one source document and returns its detached extraction
// result. The graph builds independently; Wait observes publication when a
// caller needs a complete report for the submitted workset.
func (g *session) AddDocument(ctx context.Context, document Document) pond.ResultTask[Facts] {
	tasks, err := g.enqueueDocuments(ctx, document)
	if err != nil {
		return completedTask[Facts]{Err: err}
	}
	return tasks[0]
}

// GetDocument returns the extraction task for an already submitted Document ID.
// It reports ErrDocumentNotFound when no result exists for the ID; it never
// starts extraction implicitly. The task can be awaited without waiting for
// the complete graph.
func (g *session) GetDocument(id string) (pond.ResultTask[Facts], error) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	entry, ok := g.documentTasks[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDocumentNotFound, id)
	}
	return entry.task, nil
}

// FindAsync projects declarations from an already submitted document as soon
// as its extraction completes. These detached nodes do not imply that cross-
// document relations or the queryable graph have been published.
func (g *session) FindAsync(path string, kind NodeKind, qualifiedName string) (pond.ResultTask[[]Node], bool) {
	task, err := g.GetDocument(DocumentID(path))
	if err != nil {
		return nil, false
	}
	return mappedTask[Facts, []Node]{source: task, project: func(facts Facts) ([]Node, error) {
		nodes := make([]Node, 0)
		for _, d := range facts.Declarations {
			if kind != "" && d.Kind != kind || qualifiedName != "" && d.QualifiedName != qualifiedName {
				continue
			}
			nodes = append(nodes, Node{
				ID:   declarationID(path, d.Kind, d.QualifiedName, d.Location.StartByte),
				Kind: d.Kind, Name: d.Name, QualifiedName: d.QualifiedName,
				Language: facts.Language, Location: &d.Location,
				Markers: append([]Marker(nil), d.Markers...),
			})
		}
		return nodes, nil
	}}, true
}

func (g *session) enqueueDocuments(ctx context.Context, documents ...Document) ([]pond.ResultTask[Facts], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()

	seen := make(map[string]Document, len(documents))
	g.mu.RLock()
	for _, document := range documents {
		if err := document.validate(); err != nil {
			g.mu.RUnlock()
			return nil, err
		}
		if old, ok := seen[document.Path]; ok && !document.same(old) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		if old, ok := g.documentTasks[document.ID()]; ok && !old.failed && !document.same(old.document) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		if old, ok := g.documents[document.Path]; ok && !document.matches(old) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		seen[document.Path] = document
	}
	boundaries := make(map[string]Document, len(g.documents)+len(g.documentTasks)+len(seen))
	for p, f := range g.documents {
		boundaries[p] = Document{Path: p, Gitlink: f.Gitlink}
	}
	for _, task := range g.documentTasks {
		if !task.failed {
			boundaries[task.document.Path] = task.document
		}
	}
	for p, d := range seen {
		boundaries[p] = d
	}
	boundaryErr := validateDocumentBoundaries(boundaries)
	g.mu.RUnlock()
	if boundaryErr != nil {
		return nil, boundaryErr
	}

	tasks := make([]pond.ResultTask[Facts], 0, len(documents))
	newDocuments := false
	for _, document := range documents {
		if old, ok := g.documentTasks[document.ID()]; ok && !old.failed {
			tasks = append(tasks, old.task)
			continue
		}
		if g.asyncPool == nil {
			g.asyncPool = pond.NewResultPool[Facts](g.opts.BuildConcurrency, pond.WithQueueSize(2*g.opts.BuildConcurrency))
		}
		if g.pendingWorkers == nil {
			g.pendingWorkers = &sync.WaitGroup{}
		}
		owned := Document{Path: document.Path, Content: bytes.Clone(document.Content), Gitlink: document.Gitlink}
		workers := g.pendingWorkers
		workers.Add(1)
		task := g.asyncPool.SubmitErr(func() (Facts, error) {
			defer workers.Done()
			return g.Extract(ctx, owned)
		})
		g.documentTasks[owned.ID()] = documentTask{document: owned, ctx: ctx, task: task}
		g.pending = append(g.pending, owned)
		tasks = append(tasks, task)
		newDocuments = true
	}
	if newDocuments {
		work := &buildWork{done: make(chan struct{})}
		g.pendingWorks = append(g.pendingWorks, work)
		g.latestWork = work
		if !g.building {
			g.building = true
			go g.buildQueued()
		}
	}
	return tasks, nil
}

// buildQueued continuously consumes admitted batches. Parsing remains bounded
// by one pool; only the coordinator assembles and publishes graph state.
func (g *session) buildQueued() {
	for {
		g.asyncMu.Lock()
		if len(g.pending) == 0 {
			// All admitted workers have been joined before their build completes.
			// Stop the idle pool while admission is locked, then release it.
			if g.asyncPool != nil {
				g.asyncPool.StopAndWait()
				g.asyncPool = nil
			}
			g.building = false
			g.asyncMu.Unlock()
			return
		}
		g.asyncMu.Unlock()

		report, err, works := g.buildAvailable()
		for _, work := range works {
			work.report, work.err = cloneReport(report), err
			close(work.done)
		}
	}
}

// buildAvailable joins the current extraction work and any admissions that
// arrive while it is running. This coalesces bursts of AddDocument calls
// without making Wait responsible for triggering or batching graph builds.
func (g *session) buildAvailable() (BuildReport, error, []*buildWork) {
	var documents []Document
	var works []*buildWork
	parseFailures := make(map[string]error)
	prepared := make(map[string]analysis.Facts)
	var canceled error
	var ctx context.Context
	var cancel context.CancelFunc
	var stops []func() bool
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()

	for {
		g.asyncMu.Lock()
		if len(g.pending) == 0 {
			g.asyncMu.Unlock()
			break
		}
		batch := g.pending
		workers := g.pendingWorkers
		works = append(works, g.pendingWorks...)
		tasks := make([]documentTask, len(batch))
		for i, document := range batch {
			tasks[i] = g.documentTasks[document.ID()]
		}
		g.pending, g.pendingWorkers, g.pendingWorks = nil, nil, nil
		g.asyncMu.Unlock()

		if ctx == nil {
			// Wait's context is only a wait deadline. Source contexts own
			// cancellation of the atomic background publication.
			ctx, cancel = context.WithCancel(context.WithoutCancel(tasks[0].ctx))
			defer cancel()
		}
		for _, entry := range tasks {
			stops = append(stops, context.AfterFunc(entry.ctx, cancel))
		}
		documents = append(documents, batch...)
		for i, entry := range tasks {
			if facts, err := entry.task.Wait(); err != nil {
				parseFailures[batch[i].Path] = err
				if ctxErr := entry.ctx.Err(); ctxErr != nil {
					canceled = ctxErr
				}
			} else {
				prepared[batch[i].Path] = *facts.raw
			}
		}
		// ResultTask can resolve on cancellation before its parser exits.
		workers.Wait()
		// A steady producer must not postpone publication indefinitely.
		// Never split one AddDocuments admission across publications.
		if len(documents) >= g.opts.MaxDocuments {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		g.markFailedTasks(documents)
		return g.Report(), err, works
	}
	if canceled != nil {
		g.markFailedTasks(documents)
		return g.Report(), canceled, works
	}
	report, err := g.addPrepared(ctx, parseFailures, prepared, documents...)
	if err != nil {
		g.markFailedTasks(documents)
		return report, err, works
	}
	g.markFailedPaths(parseFailures)
	return report, nil, works
}

// Wait waits for graph publication of all documents admitted before the call.
// Later admissions may share that publication and appear in its report. Wait
// never starts extraction, assembly or publication; canceling ctx only ends
// this wait.
// +spec=`Submission drives graph construction; Wait only waits for its completion`
func (g *session) Wait(ctx context.Context) (BuildReport, error) {
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	g.asyncMu.Lock()
	work := g.latestWork
	g.asyncMu.Unlock()
	if work == nil {
		return g.Report(), nil
	}
	select {
	case <-ctx.Done():
		return g.Report(), ctx.Err()
	case <-work.done:
		if err := ctx.Err(); err != nil {
			return g.Report(), err
		}
		return cloneReport(work.report), work.err
	}
}

func (g *session) markFailedTasks(documents []Document) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	for _, document := range documents {
		entry := g.documentTasks[document.ID()]
		entry.failed = true
		g.documentTasks[document.ID()] = entry
	}
}

func (g *session) markFailedPaths(failures map[string]error) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	for path := range failures {
		id := DocumentID(path)
		entry := g.documentTasks[id]
		entry.failed = true
		g.documentTasks[id] = entry
	}
}

// AddDocuments validates a batch before submission and starts background construction.
func (g *session) AddDocuments(ctx context.Context, documents ...Document) error {
	_, err := g.enqueueDocuments(ctx, documents...)
	return err
}

// factCacheEntry keys extracted facts by content identity, so a cache hit
// cannot confuse two different sources sharing one logical path.
type factCacheEntry struct {
	hash  [32]byte
	facts analysis.Facts
}

// Extract returns detached source facts for one document without publishing
// graph state. Results are cached by path and content identity: a later
// AddDocuments of the same path and content reuses them instead of parsing
// again, and facts of already loaded documents are projected without parsing.
// Source documents without a registered grammar yield file-level facts carrying an
// unsupported_language issue, mirroring how AddDocuments records them.
// Gitlinks yield only their path and commit, without attempting language parsing.
// +spec=`Exploration extraction never reparses identical snapshot content`
func (g *session) Extract(ctx context.Context, document Document) (Facts, error) {
	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}
	if err := document.validate(); err != nil {
		return Facts{}, err
	}
	hash := document.digest()
	g.factCacheMu.Lock()
	entry, cached := g.factCache[document.Path]
	g.factCacheMu.Unlock()
	if cached && entry.hash == hash {
		return projectFacts(entry.facts)
	}
	g.mu.RLock()
	staged, loaded := g.documents[document.Path]
	g.mu.RUnlock()
	if loaded && document.matches(staged) {
		return projectFacts(staged)
	}
	facts, err := g.extractor.extract(ctx, document)
	if err != nil {
		return Facts{}, err
	}
	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}
	g.factCacheMu.Lock()
	g.factCache[document.Path] = factCacheEntry{hash: hash, facts: facts}
	g.factCacheMu.Unlock()
	return projectFacts(facts)
}

// cachedFacts returns a cached extraction for identical content and consumes
// the entry: once staged, the graph's retained facts become the authoritative
// copy, so the cache stays bounded to explored-but-not-yet-added documents.
func (g *session) cachedFacts(document Document) (analysis.Facts, bool) {
	g.factCacheMu.Lock()
	defer g.factCacheMu.Unlock()
	entry, ok := g.factCache[document.Path]
	if !ok || entry.hash != document.digest() {
		return analysis.Facts{}, false
	}
	delete(g.factCache, document.Path)
	return entry.facts, true
}

// stageDocuments reserves capacity for each extraction window before starting
// workers. Failed parses release their reservations before the next window, so
// parallelism does not change which documents fit the existing source budget.
// Documents without a registered grammar are staged as file-level facts without
// parsing; the file still enters the graph and the coverage gap stays visible
// as an unsupported_language issue on that file.
// +spec=`Workers own independent extraction results and never mutate graph maps`
func (g *session) stageDocuments(ctx context.Context, documents []Document, staged map[string]analysis.Facts, failures map[string]Diagnostic, total int64, parseFailures map[string]error, prepared map[string]analysis.Facts) error {
	for next := 0; next < len(documents); {
		batch := make([]Document, 0, min(g.opts.BuildConcurrency, len(documents)-next))
		var reserved int64
		for next < len(documents) && len(batch) < g.opts.BuildConcurrency {
			if err := ctx.Err(); err != nil {
				return err
			}
			document := documents[next]
			name, data := document.Path, document.Content
			issue := func(code, message string) {
				failures[name] = Diagnostic{Code: code, Message: message, Subject: DocumentSubject,
					Location: location(pipeline.DocumentOnly(name, data), analysis.Span{End: len(data)})}
			}
			if !g.allowed(name) {
				issue("out_of_scope", "file is outside allowed scope")
				next++
				continue
			}
			if document.size() > g.opts.MaxDocumentBytes {
				return fmt.Errorf("%w: file %s exceeds byte limit", ErrBuildBudget, name)
			}
			if old, exists := staged[name]; exists {
				if !document.matches(old) {
					return fmt.Errorf("%w: %s", ErrSnapshotChanged, name)
				}
				delete(failures, name)
				next++
				continue
			}
			if parseErr, failed := parseFailures[name]; failed {
				issue("parse_error", parseErr.Error())
				next++
				continue
			}
			var budgetErr error
			if len(staged)+len(batch) >= g.opts.MaxDocuments {
				budgetErr = fmt.Errorf("%w: file limit %d", ErrBuildBudget, g.opts.MaxDocuments)
			} else if document.size() > g.opts.MaxSourceBytes-total-reserved {
				budgetErr = fmt.Errorf("%w: source byte limit", ErrBuildBudget)
			}
			if budgetErr != nil {
				if len(batch) == 0 {
					return budgetErr
				}
				// Pending parses may fail and free capacity. Finish them before
				// deciding whether this document exceeds the batch's budget.
				break
			}
			// Extraction results are passed directly across the phase boundary.
			facts, ok := prepared[name]
			if !ok {
				facts, ok = g.cachedFacts(document)
			} else {
				g.cachedFacts(document) // Release legacy exploration retention after admission.
			}
			if ok {
				staged[name] = facts
				total += document.size()
				next++
				continue
			}
			batch = append(batch, document)
			reserved += document.size()
			next++
		}
		results := g.extractBatch(ctx, batch)
		if err := ctx.Err(); err != nil {
			return err
		}
		// Merge in input order, independent of worker completion order. Assembly
		// and cross-document resolution only begin after all windows finish.
		for i, result := range results {
			name := batch[i].Path
			if result.err != nil {
				failures[name] = Diagnostic{Code: "parse_error", Message: result.err.Error(), Subject: DocumentSubject,
					Location: location(pipeline.DocumentOnly(name, batch[i].Content), analysis.Span{End: len(batch[i].Content)})}
				continue
			}
			staged[name] = result.facts
			total += int64(len(result.facts.Source) + len(result.facts.Gitlink))
			delete(failures, name)
		}
	}
	return ctx.Err()
}

type extractionResult struct {
	facts analysis.Facts
	err   error
}

func (g *session) extractBatch(ctx context.Context, documents []Document) []extractionResult {
	results := make([]extractionResult, len(documents))
	run := func(i int) {
		if err := ctx.Err(); err != nil {
			results[i].err = err
			return
		}
		document := documents[i]
		results[i].facts, results[i].err = g.extractor.extract(ctx, document)
	}
	if len(documents) == 1 {
		run(0)
		return results
	}
	var workers sync.WaitGroup
	for i := range documents {
		workers.Go(func() { run(i) })
	}
	// Parsers have individual timeouts. Join workers even on cancellation so no
	// parser, AST or source copy outlives this extraction window.
	workers.Wait()
	return results
}

// addPrepared publishes one explicit document batch atomically. Successful
// asynchronous extractions are supplied explicitly; parse failures are
// supplied separately so the background builder does not retry them.
func (g *session) addPrepared(ctx context.Context, parseFailures map[string]error, prepared map[string]analysis.Facts, documents ...Document) (BuildReport, error) {
	g.buildMu.Lock()
	defer g.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	for _, document := range documents {
		if err := document.validate(); err != nil {
			return g.Report(), err
		}
	}
	staged := make(map[string]analysis.Facts, len(g.documents))
	var total int64
	for p, f := range g.documents {
		staged[p] = f
		total += int64(len(f.Source) + len(f.Gitlink))
	}
	failures := map[string]Diagnostic{}
	for p, d := range g.failures {
		failures[p] = d
	}
	if err := g.stageDocuments(ctx, documents, staged, failures, total, parseFailures, prepared); err != nil {
		return g.Report(), err
	}
	nodes, relations, report, err := g.assemble(ctx, staged, failures)
	if err != nil {
		return g.Report(), err
	}
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	// Publish only after the complete batch, including all edge properties, exists.
	g.mu.Lock()
	defer g.mu.Unlock()
	g.documents, g.failures = staged, failures
	g.sourceBytes = 0
	for _, f := range staged {
		g.sourceBytes += int64(len(f.Source) + len(f.Gitlink))
	}
	g.result = newGraph(g.snapshot, g.opts, nodes, relations, report)
	return cloneReport(report), nil
}
