package syntax

import (
	"github.com/compforge/codegraph/internal/analysis"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
)

var definitionCapture = regexp.MustCompile(`@definition\.([a-z_]+)`)

// DeclarationKinds reads advertised capture data for the requested grammar.
// Runtime omissions and unsupported categories still have diagnostics.
func DeclarationKinds(entry grammars.LangEntry) []string {
	seen := map[string]bool{}
	for _, match := range definitionCapture.FindAllStringSubmatch(entry.TagsQuery, -1) {
		if kind := analysis.ConcreteKind(match[1]); kind != "" {
			seen[kind] = true
		}
	}
	// The outliner refines these generic captures using the declaration shape.
	query := entry.TagsQuery
	for node, kind := range map[string]string{"enum_declaration": "Enum", "enum_item": "Enum", "record_declaration": "Record", "struct_item": "Struct", "struct_specifier": "Struct"} {
		if strings.Contains(query, "("+node) {
			seen[kind] = true
		}
	}
	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
