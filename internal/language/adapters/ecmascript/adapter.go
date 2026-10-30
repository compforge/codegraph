package ecmascript

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/module"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Adapter struct{}

func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	f, extracted, err := (syntax.ModuleExtractor{Dialect: Dialect{}}).Extract(ctx, f, tree, entry)
	if err == nil {
		bindNamespaceUses(&f, tree)
		err = extractResourceReads(ctx, &f, tree, extracted)
	}
	return f, err
}
func (Adapter) Compatible(lang string) bool {
	return lang == "javascript" || lang == "typescript" || lang == "tsx"
}
func (Adapter) ImportConfidence(imp analysis.Import, n int) analysis.Confidence {
	if n > 1 {
		return "scoped"
	}
	return "exact"
}
func (Adapter) ExportName(f Facts, name string) (string, bool) {
	s, ok := f.Exports[name]
	return s, ok
}
func (Adapter) NestedImport(imp analysis.Import) bool { return false }
func (a Adapter) Bind(ctx context.Context, s analysis.BuildScope, index *analysis.Index, limit int) (analysis.BindResult, error) {
	result, err := (module.Binder{Policy: a}).Bind(ctx, s, index, limit)
	if err != nil {
		return result, err
	}
	err = bindResourceReads(ctx, s, index, &result)
	return result, err
}
