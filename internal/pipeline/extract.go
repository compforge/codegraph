package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
)

func lineStarts(source []byte) []int {
	starts := []int{0}
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// DocumentOnly records a document that has no registered grammar. The file still
// enters the graph as a file-level fact — without parsing, so no declarations,
// imports, or calls — mirroring how reference code-graph indexers track
// file-level-only languages (stored file record, zero symbol nodes).
func DocumentOnly(name string, source []byte) analysis.Facts {
	return analysis.Facts{Path: name, Source: source, LineStarts: lineStarts(source)}
}

// Analyze releases the syntax tree before returning detached facts. Language
// detection is registry-driven; language-specific binding rules never leak into
// the graph model or the batch publication path.
//
// +spec=`Parsing observes caller cancellation and the per-document timeout; incomplete syntax trees are rejected.`
// +why=`The parse budget ends before fact extraction, which continues under the caller context.`
func Analyze(ctx context.Context, name string, source []byte, timeout time.Duration) (analysis.Facts, error) {
	f := analysis.Facts{Path: name, Source: source}
	f.LineStarts = lineStarts(source)
	if err := ctx.Err(); err != nil {
		return f, err
	}
	entry := language.Detect(name)
	if entry == nil {
		return f, fmt.Errorf("no grammar for %s", name)
	}
	f.Language = entry.Name
	parseCtx, cancel := context.WithTimeout(ctx, timeout)
	tree, err := entry.ParseStrict(parseCtx, source)
	cancel()
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		return f, fmt.Errorf("parse %s: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return f, err
	}
	if tree == nil || tree.RootNode() == nil || tree.RootNode().HasErrorOrMissing() {
		return f, fmt.Errorf("parse %s: incomplete syntax tree", name)
	}
	return language.Lookup(f.Language).Extractor.Extract(ctx, f, tree, *entry)
}
