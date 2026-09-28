package codegraph

import (
	"bytes"
	"context"
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/compforge/codegraph/internal/pipeline"
)

// extractMaterial owns classification and raw extraction only. Callers own
// cache policy, admission, scheduling, and atomic publication.
func (g *Graph) extractMaterial(ctx context.Context, document Document) (analysis.Facts, error) {
	if err := ctx.Err(); err != nil {
		return analysis.Facts{}, err
	}
	var facts analysis.Facts
	if document.Gitlink != "" {
		facts = pipeline.DocumentOnly(document.Path, nil)
		facts.Gitlink = document.Gitlink
	} else if language.Detect(document.Path) == nil {
		facts = pipeline.DocumentOnly(document.Path, bytes.Clone(document.Content))
		facts.Issues = append(facts.Issues, analysis.Issue{Code: "unsupported_language", Message: "no registered grammar for file", Subject: "document", Span: analysis.Span{End: len(document.Content)}})
	} else {
		if parseObserver != nil {
			parseObserver(document.Path)
		}
		var err error
		facts, err = pipeline.Analyze(ctx, document.Path, bytes.Clone(document.Content), g.opts.ParseTimeout)
		if err != nil {
			return analysis.Facts{}, err
		}
	}
	return facts, ctx.Err()
}
