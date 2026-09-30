package extract

import (
	"bytes"
	"context"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/compforge/codegraph/internal/pipeline"
)

// extractMaterial owns classification and raw extraction only. Callers own
// admission, scheduling, and atomic publication.
func (g *Extractor) extractMaterial(ctx context.Context, document Document) (analysis.Facts, error) {
	if err := ctx.Err(); err != nil {
		return analysis.Facts{}, err
	}
	var key extractionKey
	if g.opts.Cache != nil {
		key = extractionKey{path: document.Path, digest: document.digest()}
		if facts, ok := g.opts.Cache.get(key); ok {
			return facts, ctx.Err()
		}
	}
	var facts analysis.Facts
	if document.Gitlink != "" {
		facts = pipeline.DocumentOnly(document.Path, nil)
		facts.Gitlink = document.Gitlink
	} else if language.Detect(document.Path) == nil {
		facts = pipeline.DocumentOnly(document.Path, bytes.Clone(document.Content))
		facts.Issues = append(facts.Issues, analysis.Issue{Code: "unsupported_language", Message: "no registered grammar for file", Subject: "document", Span: analysis.Span{End: len(document.Content)}})
	} else {
		if ParseObserver != nil {
			ParseObserver(document.Path)
		}
		var err error
		facts, err = pipeline.Analyze(ctx, document.Path, bytes.Clone(document.Content), g.opts.ParseTimeout)
		if err != nil {
			return analysis.Facts{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return analysis.Facts{}, err
	}
	g.opts.Cache.put(key, facts)
	return facts, nil
}
