package codegraph

import (
	"context"

	"github.com/alitto/pond/v2"
)

// AddDocuments is the legacy incremental wrapper. Builder.Build results reject
// mutation; New and the package-level Build retain document admission support.
func (g *Graph) AddDocuments(ctx context.Context, docs ...Document) error {
	if g.legacy == nil {
		return ErrReadOnly
	}
	return g.legacy.AddDocuments(ctx, docs...)
}
func (g *Graph) AddDocument(ctx context.Context, doc Document) pond.ResultTask[Facts] {
	if g.legacy == nil {
		return completedTask[Facts]{err: ErrReadOnly}
	}
	return g.legacy.AddDocument(ctx, doc)
}
func (g *Graph) Extract(ctx context.Context, doc Document) (Facts, error) {
	if g.legacy == nil {
		return Facts{}, ErrReadOnly
	}
	return g.legacy.Extract(ctx, doc)
}
func (g *Graph) GetDocument(id string) (pond.ResultTask[Facts], error) {
	if g.legacy == nil {
		return nil, ErrDocumentNotFound
	}
	return g.legacy.GetDocument(id)
}
func (g *Graph) FindAsync(path string, kind NodeKind, name string) (pond.ResultTask[[]Node], bool) {
	if g.legacy == nil {
		return nil, false
	}
	return g.legacy.FindAsync(path, kind, name)
}
func (g *Graph) Wait(ctx context.Context) (BuildReport, error) {
	if g.legacy == nil {
		return g.Report(), ctx.Err()
	}
	return g.legacy.Wait(ctx)
}
