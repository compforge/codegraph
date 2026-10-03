package generic

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Adapter struct{}

func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	f, err := syntax.Outline(ctx, f, tree, entry)
	if err == nil {
		f.Issues = append(f.Issues, analysis.Issue{Code: "unsupported_resolution", Message: "declaration outline only; reference resolution and markers are not implemented for " + f.Language, Subject: "document", Span: analysis.Span{End: len(f.Source)}})
	}
	return f, err
}
func Describe(entry grammars.LangEntry) analysis.Capability {
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	return analysis.Capability{Language: entry.Name, Declarations: syntax.DeclarationKinds(entry), Relations: []string{"declares", "contains", "encloses"}, Limitations: []string{"outline is limited to grammar tags; runtime omissions are diagnostics", "syntax/outline fallback only; reference resolution and markers are not implemented; builds report partial coverage"}}
}
