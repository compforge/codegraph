package syntax

import (
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
)

func ConcreteKind(kind string) string {
	switch kind {
	case "function", "method", "struct", "interface", "field", "type", "class",
		"constructor", "constant", "variable", "module", "enum", "record",
		"namespace", "property", "trait", "macro", "union":
		return strings.ToUpper(kind[:1]) + kind[1:]
	case "type_alias":
		return "TypeAlias"
	}
	return ""
}

var definitionCapture = regexp.MustCompile(`@definition\.([a-z_]+)`)

// DeclarationKinds reads advertised capture data for the requested grammar.
// Runtime omissions and unsupported categories still have diagnostics.
func DeclarationKinds(entry grammars.LangEntry) []string {
	seen := map[string]bool{}
	for _, match := range definitionCapture.FindAllStringSubmatch(entry.TagsQuery, -1) {
		if kind := ConcreteKind(match[1]); kind != "" {
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
