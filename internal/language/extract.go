package language

import (
	"context"
	"fmt"
	"time"

	"github.com/compforge/codegraph/internal/analysis"
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

// DocumentOnly records source locations for a document without parsing it.
func DocumentOnly(name string, source []byte) analysis.Facts {
	return analysis.Facts{Path: name, Source: source, LineStarts: lineStarts(source)}
}

// Extract releases the syntax tree before returning detached facts. Language
// detection is registry-driven; language-specific binding rules never leak into
// the graph model or the batch publication path.
//
// +spec=`Parsing observes caller cancellation and the per-document timeout; incomplete syntax trees are rejected.`
// +why=`The parse budget ends before fact extraction, which continues under the caller context.`
func Extract(ctx context.Context, name string, source []byte, timeout time.Duration) (analysis.Facts, error) {
	f := DocumentOnly(name, source)
	if err := ctx.Err(); err != nil {
		return f, err
	}
	entry := Detect(name)
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
	return Lookup(f.Language).Extractor.Extract(ctx, f, tree, *entry)
}
