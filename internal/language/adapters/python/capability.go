package python

import (
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	"github.com/odvcencio/gotreesitter/grammars"
)

func Describe(entry grammars.LangEntry) analysis.Capability {
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	cap := analysis.Capability{Documentation: true, Language: entry.Name, Declarations: syntax.DeclarationKinds(entry), Relations: []string{"declares", "contains", "encloses", "imports", "calls", "references", "extends"}, Markers: []string{"spec", "case", "rule", "link", "doc"}, Limitations: []string{"documentation preserves the first plain string expression in a class/function body, including nested and decorated declarations; bytes, f-strings and later strings are excluded", "outline is limited to grammar tags; runtime omissions are diagnostics"}}
	cap.Limitations = append(cap.Limitations, "named base types bind extends; TS/TSX explicit interfaces bind implements; inherited methods follow bound extends edges as scoped targets; runtime MRO and dynamic base expressions are not evaluated", "calls resolve to unshadowed local/imported functions; class constructors and receiver methods have syntax-based candidates; arbitrary runtime dispatch remains unresolved", "imports use local source paths and explicit export bindings; dependency configuration, runtime paths and third-party modules are not evaluated; Python absolute imports are scoped")
	cap.Organizations = []string{"Package", "Module"}
	for _, k := range cap.Declarations {
		if k == "Function" {
			cap.Declarations = append(cap.Declarations, "Method")
			sort.Strings(cap.Declarations)
			break
		}
	}
	return cap
}
