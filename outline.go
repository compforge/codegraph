package codegraph

import (
	"context"
	"fmt"
	"slices"

	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
)

// Outline extracts a document's lexical structure without building a Graph.
// It shares Extract's parsing, limits and optional cache. Returned symbols and
// report use gotreesitter's contract; a declined or truncated query is reported
// in OutlineReport. Unsupported materials and extraction failures return errors.
func (e *Extractor) Outline(ctx context.Context, doc Document) ([]gts.OutlineSymbol, gts.OutlineReport, error) {
	f, err := e.extract(ctx, doc)
	if err != nil {
		return nil, gts.OutlineReport{}, err
	}
	return documentOutline(f)
}

// Outline returns an independent view of the document's extracted structure.
// Children express lexical nesting; Owner is an unbound nonlexical owner name.
// Kinds, ranges and coverage follow gotreesitter, not Graph's semantic members.
// No omissions does not prove complete language coverage. No graph is required,
// and modifying this result cannot change cached facts or later graph builds.
// +spec=Document outlines retain lexical structure and upstream coverage independently of graph binding.
func (f Facts) Outline() ([]gts.OutlineSymbol, gts.OutlineReport, error) {
	if f.raw == nil {
		return nil, gts.OutlineReport{}, fmt.Errorf("facts must be produced by an Extractor")
	}
	return documentOutline(*f.raw)
}

func documentOutline(f analysis.Facts) ([]gts.OutlineSymbol, gts.OutlineReport, error) {
	if f.OutlineError != nil {
		return nil, f.OutlineReport, fmt.Errorf("document %s: %w", f.Path, f.OutlineError)
	}
	return cloneOutline(f.Outline), f.OutlineReport, nil
}

func cloneOutline(symbols []gts.OutlineSymbol) []gts.OutlineSymbol {
	out := slices.Clone(symbols)
	for i := range out {
		out[i].Children = cloneOutline(out[i].Children)
	}
	return out
}
