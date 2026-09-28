package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	"github.com/odvcencio/gotreesitter/grammars"
)

func Describe(entry grammars.LangEntry) analysis.Capability {
	entry.TagsQuery = query(entry)
	cap := analysis.Capability{Language: entry.Name, Organizations: []string{"Module"}, Declarations: syntax.DeclarationKinds(entry), Relations: []string{"declares", "contains", "imports", "calls", "references", "extends"}, Markers: []string{"spec", "case", "rule", "link", "doc"}, Limitations: []string{"outline is limited to grammar tags; runtime omissions are diagnostics"}}
	cap.Limitations = append(cap.Limitations, "named base types bind extends; TS/TSX explicit interfaces bind implements; inherited methods follow bound extends edges as candidates; runtime MRO and dynamic base expressions are not evaluated", "calls resolve to unshadowed local/imported functions; class constructors and receiver methods have syntax-based candidates; arbitrary runtime dispatch remains unresolved", "imports use local source paths and explicit export bindings; dependency configuration, runtime paths and third-party modules are not evaluated; Python absolute imports are candidates")
	if entry.Name == "typescript" || entry.Name == "tsx" {
		cap.Relations = append(cap.Relations, "implements")
	}
	return cap
}
