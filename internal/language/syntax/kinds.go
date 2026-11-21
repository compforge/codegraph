package syntax

import (
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/odvcencio/gotreesitter/grammars"
)

// DeclarationKinds reads advertised capture data for the requested grammar.
// Runtime omissions and unsupported categories still have diagnostics.
func DeclarationKinds(entry grammars.LangEntry) []string {
	seen := map[string]bool{}
	for _, row := range declarationRules(entry) {
		kind := declarationKind(row.Kind, row.NodeType, row.Shape)
		if concrete := analysis.ConcreteKind(kind); concrete != "" {
			seen[string(concrete)] = true
		}
	}

	if entry.Language != nil && entry.TagsQuery != "" {
		if outline, err := outlineProgram(entry.Language(), entry); err == nil {
			for _, captured := range outline.DefinitionKinds() {
				if kind := analysis.ConcreteKind(captured); kind != "" {
					seen[string(kind)] = true
				}
			}
		}
	}
	// The outliner refines these generic captures using the declaration shape.
	query := entry.TagsQuery
	for node, kind := range map[string]string{"enum_declaration": "Enum", "enum_item": "Enum", "record_declaration": "Record", "struct_item": "Struct", "struct_specifier": "Struct"} {
		if strings.Contains(query, "("+node) {
			seen[string(kind)] = true
		}
	}
	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
