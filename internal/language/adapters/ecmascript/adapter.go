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

func query(entry grammars.LangEntry) string {
	q := grammars.ResolveTagsQuery(entry)
	q += "\n(variable_declarator name: (identifier) @name) @definition.variable"
	if entry.Name != "javascript" {
		q += "\n(type_alias_declaration name: (type_identifier) @name) @definition.type_alias"
	}
	return q
}
func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	entry.TagsQuery = query(entry)
	return (syntax.ModuleExtractor{Dialect: Dialect{}}).Extract(ctx, f, tree, entry)
}
func (Adapter) Compatible(lang string) bool {
	return lang == "javascript" || lang == "typescript" || lang == "tsx"
}
func (Adapter) ImportConfidence(imp analysis.Import, n int) string {
	if n > 1 {
		return "candidate"
	}
	return "exact"
}
func (Adapter) ExportName(f Facts, name string) (string, bool) {
	s, ok := f.Exports[name]
	return s, ok
}
func (Adapter) NestedImport(imp analysis.Import) bool { return false }
func (a Adapter) Bind(ctx context.Context, s analysis.Scope, index *analysis.Index, limit int) (analysis.Binding, error) {
	return (module.Binder{Policy: a}).Bind(ctx, s, index, limit)
}
