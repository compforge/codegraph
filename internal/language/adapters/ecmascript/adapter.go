package ecmascript

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

// query owns the declaration shapes supported by this adapter. Method names
// must immediately precede parameters (or type parameters): a modifier can be
// exposed as an extra property_identifier, including an incorrect name field.
func query(entry grammars.LangEntry) string {
	q := `
(function_declaration name: (identifier) @name) @definition.function
(generator_function_declaration name: (identifier) @name) @definition.function
(method_definition [(property_identifier) (private_property_identifier)] @name . (comment)* . [(formal_parameters) (type_parameters)]) @definition.method
(variable_declarator name: (identifier) @name) @definition.variable
(for_in_statement kind: ["var" "let" "const"] left: (identifier) @name @definition.variable)
(field_definition property: [(property_identifier) (private_property_identifier)] @name) @definition.field
(catch_clause parameter: (identifier) @name @definition.variable)`
	if entry.Name == "javascript" {
		// JavaScript has no type_parameters node in its grammar.
		q = strings.ReplaceAll(q, "[(formal_parameters) (type_parameters)]", "(formal_parameters)")
		q += "\n(class_declaration name: (identifier) @name) @definition.class"
	} else {
		q = strings.ReplaceAll(q, "field_definition property:", "public_field_definition name:")
		q += `
(internal_module name: (identifier) @name) @definition.namespace
(property_signature name: [(property_identifier) (private_property_identifier)] @name) @definition.property
(method_signature name: (property_identifier) @name) @definition.method
(class_declaration name: (type_identifier) @name) @definition.class
(interface_declaration name: (type_identifier) @name) @definition.interface
(enum_declaration name: (identifier) @name) @definition.type
(type_alias_declaration name: (type_identifier) @name) @definition.type_alias`
	}
	return q
}
func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	entry.TagsQuery = query(entry)
	f, err := (syntax.ModuleExtractor{Dialect: Dialect{}}).Extract(ctx, f, tree, entry)
	if err == nil {
		bindNamespaceUses(&f, tree)
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
	return (module.Binder{Policy: a}).Bind(ctx, s, index, limit)
}
