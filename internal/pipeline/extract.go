package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	gts "github.com/odvcencio/gotreesitter"
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
	if entry.Language == nil {
		return f, fmt.Errorf("grammar %s has no loader", entry.Name)
	}
	lang := entry.Language()
	if lang == nil {
		return f, fmt.Errorf("grammar %s is unavailable", entry.Name)
	}
	p := gts.NewParser(lang)
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	p.SetTimeoutMicros(uint64(max(timeout.Microseconds(), 1)))
	var tree *gts.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		tree, err = p.ParseWithTokenSourceStrict(source, entry.TokenSourceFactory(source, lang))
	} else {
		tree, err = p.ParseStrict(source)
	}
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
