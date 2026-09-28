package python

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/module"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Adapter struct{}

func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	f, err := (syntax.ModuleExtractor{Dialect: Dialect{}}).Extract(ctx, f, tree, entry)
	if err == nil {
		f.Statements = pythonProgram(&f, tree)
	}
	return f, err
}
func (Adapter) Compatible(lang string) bool { return lang == "python" }
func (Adapter) ImportConfidence(imp analysis.Import, n int) analysis.Confidence {
	if n > 1 || imp.Relative == 0 {
		return "scoped"
	}
	return "exact"
}
func (Adapter) ExportName(f Facts, name string) (string, bool) { return name, true }
func (Adapter) NestedImport(imp analysis.Import) bool {
	return imp.Alias == "" && strings.Contains(imp.Path, ".")
}
func (a Adapter) Bind(ctx context.Context, s analysis.BuildScope, index *analysis.Index, limit int) (analysis.BindResult, error) {
	return (module.Binder{Policy: a}).Bind(ctx, s, index, limit)
}
