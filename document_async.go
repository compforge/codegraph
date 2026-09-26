package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/alitto/pond/v2"
)

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

type completedTask[T any] struct {
	value T
	err   error
}

var completed = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

func (t completedTask[T]) Done() <-chan struct{} { return completed }
func (t completedTask[T]) Wait() (T, error)      { return t.value, t.err }

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
func (g *Graph) AddDocument(ctx context.Context, document Document) pond.ResultTask[Facts] {
	tasks, err := g.enqueueDocuments(ctx, document)
	if err != nil {
		return completedTask[Facts]{err: err}
	}
	return tasks[0]
}

// GetDocument returns the extraction task for an already submitted Document ID.
// It reports ErrDocumentNotFound when no result exists for the ID; it never
// starts extraction implicitly. The task can be awaited without waiting for
// the complete graph.
func (g *Graph) GetDocument(id string) (pond.ResultTask[Facts], error) {
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
func (g *Graph) FindAsync(path string, kind NodeKind, qualifiedName string) (pond.ResultTask[[]Node], bool) {
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
				Language: facts.Language, Location: d.Location,
				Markers: append([]Marker(nil), d.Markers...),
			})
		}
		return nodes, nil
	}}, true
}

func (g *Graph) enqueueDocuments(ctx context.Context, documents ...Document) ([]pond.ResultTask[Facts], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()

	seen := make(map[string]Document, len(documents))
	g.mu.RLock()
	for _, document := range documents {
		if !fs.ValidPath(document.Path) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("invalid source path %q", document.Path)
		}
		if old, ok := seen[document.Path]; ok && !bytes.Equal(old.Content, document.Content) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		if old, ok := g.documentTasks[document.ID()]; ok && !old.failed && !bytes.Equal(old.document.Content, document.Content) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		if old, ok := g.documents[document.Path]; ok && !bytes.Equal(old.Source, document.Content) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		seen[document.Path] = document
	}
	g.mu.RUnlock()

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
		owned := Document{Path: document.Path, Content: bytes.Clone(document.Content)}
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
func (g *Graph) buildQueued() {
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
func (g *Graph) buildAvailable() (BuildReport, error, []*buildWork) {
	var documents []Document
	var works []*buildWork
	parseFailures := make(map[string]error)
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
			if _, err := entry.task.Wait(); err != nil {
				parseFailures[batch[i].Path] = err
				if ctxErr := entry.ctx.Err(); ctxErr != nil {
					canceled = ctxErr
				}
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
	report, err := g.addPrepared(ctx, parseFailures, documents...)
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
func (g *Graph) Wait(ctx context.Context) (BuildReport, error) {
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

func (g *Graph) markFailedTasks(documents []Document) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	for _, document := range documents {
		entry := g.documentTasks[document.ID()]
		entry.failed = true
		g.documentTasks[document.ID()] = entry
	}
}

func (g *Graph) markFailedPaths(failures map[string]error) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	for path := range failures {
		id := DocumentID(path)
		entry := g.documentTasks[id]
		entry.failed = true
		g.documentTasks[id] = entry
	}
}
