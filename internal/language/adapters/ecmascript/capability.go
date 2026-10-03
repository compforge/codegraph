package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	"github.com/odvcencio/gotreesitter/grammars"
)

func Describe(entry grammars.LangEntry) analysis.Capability {
	entry.TagsQuery = query(entry)
	cap := analysis.Capability{Documentation: true, SourceItems: []string{"Import", "Export"}, Language: entry.Name, Organizations: []string{"Module"}, Declarations: syntax.DeclarationKinds(entry), References: []string{"calls", "references", "extends", "decorates"}, Relations: []string{"occurs_in", "aliases", "exports", "decorates", "declares", "contains", "encloses", "imports", "calls", "references", "extends"}, Markers: []string{"spec", "case", "rule", "link", "doc"}, Limitations: []string{"documentation preserves immediately leading standalone JSDoc comments on supported declarations, including export wrappers and single variable declarations; detached and trailing comments are excluded", "declarations include class fields; unsupported destructuring and computed members are reported when encountered"}}
	cap.Limitations = append(cap.Limitations, "named base types bind extends; TS/TSX explicit interfaces bind implements; inherited methods follow bound extends edges as scoped targets; runtime MRO and dynamic base expressions are not evaluated", "calls resolve to unshadowed local/imported functions; class constructors and receiver methods have syntax-based candidates; arbitrary runtime dispatch remains unresolved", "imports use local source paths and explicit export bindings; dependency configuration, runtime paths and third-party modules are not evaluated; Python absolute imports are scoped")
	if entry.Name == "typescript" || entry.Name == "tsx" {
		cap.Relations = append(cap.Relations, "implements")
		cap.References = append(cap.References, "implements")
		cap.Limitations = append(cap.Limitations, "declarations include interface properties/methods and local named namespaces with lexical scope and explicit exports; unsupported signature forms are reported when encountered; namespace merging, dotted declarations and ambient modules are not resolved")
	}
	return cap
}
