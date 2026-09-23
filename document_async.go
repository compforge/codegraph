package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"

	"github.com/alitto/pond/v2"
)

// documentTask retains the submitted bytes so repeated paths cannot silently
// change the source snapshot while their extraction is in flight.
type documentTask struct {
	document Document
	ctx      context.Context
	task     pond.ResultTask[Facts]
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
// result. Completion does not mean the graph has been published; call Flush
// once the caller has submitted the complete dependency workset.
func (g *Graph) AddDocument(ctx context.Context, document Document) pond.ResultTask[Facts] {
	tasks, err := g.enqueueDocuments(ctx, document)
	if err != nil {
		return completedTask[Facts]{err: err}
	}
	return tasks[0]
}

// GetDocument returns the extraction task for an already submitted
// Document ID. It can be awaited before Flush to discover more dependencies.
func (g *Graph) GetDocument(id string) (pond.ResultTask[Facts], bool) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	entry, ok := g.documentTasks[id]
	return entry.task, ok
}

// FindAsync projects declarations from an already submitted document as soon
// as its extraction completes. These detached nodes do not imply that cross-
// document relations or the queryable graph have been published.
func (g *Graph) FindAsync(path string, kind NodeKind, qualifiedName string) (pond.ResultTask[[]Node], bool) {
	task, ok := g.GetDocument(FileID(path))
	if !ok {
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
	if g.flushing {
		return nil, ErrBuildInProgress
	}

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
		if old, ok := g.documentTasks[document.ID()]; ok && !bytes.Equal(old.document.Content, document.Content) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		if old, ok := g.files[document.Path]; ok && !bytes.Equal(old.Source, document.Content) {
			g.mu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
		}
		seen[document.Path] = document
	}
	g.mu.RUnlock()

	tasks := make([]pond.ResultTask[Facts], 0, len(documents))
	for _, document := range documents {
		if old, ok := g.documentTasks[document.ID()]; ok {
			tasks = append(tasks, old.task)
			continue
		}
		if g.asyncPool == nil {
			g.asyncPool = pond.NewResultPool[Facts](g.opts.BuildConcurrency, pond.WithQueueSize(2*g.opts.BuildConcurrency))
		}
		owned := Document{Path: document.Path, Content: bytes.Clone(document.Content)}
		task := g.asyncPool.SubmitErr(func() (Facts, error) { return g.Extract(ctx, owned) })
		g.documentTasks[owned.ID()] = documentTask{document: owned, ctx: ctx, task: task}
		g.pending = append(g.pending, owned)
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// Flush waits for documents submitted before this call, resolves their
// cross-document relations and atomically publishes the resulting graph.
// Flush seals the batch; new submissions are accepted after it returns.
// +spec=`Only Flush publishes a complete batch; early ResultTasks expose detached facts`
func (g *Graph) Flush(ctx context.Context) (BuildReport, error) {
	g.flushMu.Lock()
	defer g.flushMu.Unlock()
	g.asyncMu.Lock()
	g.flushing = true
	documents, pool := g.pending, g.asyncPool
	tasks := make([]documentTask, len(documents))
	for i, document := range documents {
		tasks[i] = g.documentTasks[document.ID()]
	}
	g.pending, g.asyncPool = nil, nil
	g.asyncMu.Unlock()
	defer func() {
		g.asyncMu.Lock()
		g.flushing = false
		g.asyncMu.Unlock()
	}()
	if len(documents) == 0 {
		return g.Report(), ctx.Err()
	}

	parseFailures := make(map[string]error)
	var canceled error
	for i, entry := range tasks {
		if _, err := entry.task.Wait(); err != nil {
			parseFailures[documents[i].Path] = err
			if ctxErr := entry.ctx.Err(); ctxErr != nil {
				canceled = ctxErr
			}
		}
	}
	// A canceled ResultTask may resolve before its worker exits. Join every
	// parser before publishing, rolling back, or releasing source bytes.
	pool.StopAndWait()
	if err := ctx.Err(); err != nil {
		g.forgetTasks(documents)
		return g.Report(), err
	}
	if canceled != nil {
		g.forgetTasks(documents)
		return g.Report(), canceled
	}
	report, err := g.addPrepared(ctx, parseFailures, documents...)
	if err != nil {
		g.forgetTasks(documents)
		return report, err
	}
	g.asyncMu.Lock()
	for path := range parseFailures {
		delete(g.documentTasks, FileID(path))
	}
	g.asyncMu.Unlock()
	return report, nil
}

func (g *Graph) forgetTasks(documents []Document) {
	g.asyncMu.Lock()
	defer g.asyncMu.Unlock()
	for _, document := range documents {
		delete(g.documentTasks, document.ID())
	}
}
