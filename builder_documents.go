package codegraph

import (
	"context"

	"github.com/alitto/pond/v2"
)

// Build creates a graph from one explicit source-document batch. Document
// selection and source loading belong to the caller; this entrypoint never
// discovers additional files implicitly.
func Build(ctx context.Context, snapshot string, documents []Document, opts Options) (*Graph, BuildReport, error) {
	g, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, BuildReport{}, err
	}
	if err := g.AddDocuments(ctx, documents...); err != nil {
		return nil, g.Report(), err
	}
	r, err := g.Wait(ctx)
	if err != nil {
		return nil, r, err
	}
	return g.Result(), r, nil
}

// AddDocuments admits an explicit batch and starts background graph construction.
// The batch is validated before any document is admitted. It returns after
// submission; callers can use GetDocument or FindAsync for early facts, and
// Wait to wait for complete graph publication. Input bytes are copied.
func (b *Builder) AddDocuments(ctx context.Context, docs ...Document) error {
	s, err := b.documentSession()
	if err != nil {
		return err
	}
	return s.AddDocuments(ctx, docs...)
}

// AddDocument queues one source document and returns its detached extraction
// result. The graph builds independently; Wait observes publication when a
// caller needs a complete report for the submitted workset.
func (b *Builder) AddDocument(ctx context.Context, doc Document) pond.ResultTask[Facts] {
	s, err := b.documentSession()
	if err != nil {
		return completedTask[Facts]{Err: err}
	}
	return s.AddDocument(ctx, doc)
}

// Extract returns detached source facts for one document without publishing
// graph state. Results are cached by path and content identity: a later
// AddDocuments of the same path and content reuses them instead of parsing
// again, and facts of already loaded documents are projected without parsing.
// Source documents without a registered grammar yield file-level facts carrying an
// unsupported_language issue, mirroring how AddDocuments records them.
// Gitlinks yield only their path and commit, without attempting language parsing.
// +spec=`Exploration extraction never reparses identical snapshot content`
func (b *Builder) Extract(ctx context.Context, doc Document) (Facts, error) {
	s, err := b.documentSession()
	if err != nil {
		return Facts{}, err
	}
	return s.Extract(ctx, doc)
}

// GetDocument returns the extraction task for an already submitted Document ID.
// It reports ErrDocumentNotFound when no result exists for the ID; it never
// starts extraction implicitly. The task can be awaited without waiting for
// the complete graph.
func (b *Builder) GetDocument(id string) (pond.ResultTask[Facts], error) {
	s, err := b.documentSession()
	if err != nil {
		return nil, err
	}
	return s.GetDocument(id)
}

// FindAsync projects declarations from an already submitted document as soon
// as its extraction completes. These detached nodes do not imply that cross-
// document relations or the queryable graph have been published.
func (b *Builder) FindAsync(path string, kind NodeKind, name string) (pond.ResultTask[[]Node], bool) {
	s, err := b.documentSession()
	if err != nil {
		return nil, false
	}
	return s.FindAsync(path, kind, name)
}

// Wait waits for graph publication of all documents admitted before the call.
// Later admissions may share that publication and appear in its report. Wait
// never starts extraction, assembly or publication; canceling ctx only ends
// this wait.
// +spec=`Submission drives graph construction; Wait only waits for its completion`
func (b *Builder) Wait(ctx context.Context) (BuildReport, error) {
	s, err := b.documentSession()
	if err != nil {
		return b.Report(), err
	}
	return s.Wait(ctx)
}

func (b *Builder) documentSession() (*session, error) {
	b.sessionOnce.Do(func() { b.session, b.sessionErr = newSession(b) })
	return b.session, b.sessionErr
}
