package build

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/model"
)

// addPrepared publishes one explicit document batch atomically. Successful
// asynchronous extractions are supplied explicitly; parse failures are
// supplied separately so the background builder does not retry them.
func (g *Session) addPrepared(ctx context.Context, parseFailures map[string]error, prepared map[string]analysis.Facts, documents ...extract.Document) (model.BuildReport, error) {
	g.buildMu.Lock()
	defer g.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	for _, document := range documents {
		if err := extract.Validate(document); err != nil {
			return g.Report(), err
		}
	}
	staged := make(map[string]analysis.Facts, len(g.documents))
	var total int64
	for p, f := range g.documents {
		staged[p] = f
		total += int64(len(f.Source) + len(f.Gitlink))
	}
	failures := map[string]model.Diagnostic{}
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
	return model.CloneReport(report), nil
}
