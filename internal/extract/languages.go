package extract

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
)

// Detect uses the upstream registry rather than a closed language allowlist.
// These suffixes select syntax variants not consistently covered by Linguist.
func Detect(name string) *grammars.LangEntry {
	switch strings.ToLower(path.Ext(name)) {
	case ".pyi":
		return grammars.DetectLanguageByName("python")
	case ".mts", ".cts":
		return grammars.DetectLanguageByName("typescript")
	case ".mjs", ".cjs", ".jsx":
		return grammars.DetectLanguageByName("javascript")
	}
	return grammars.DetectLanguage(name)
}

func ModuleLanguage(language string) bool {
	switch language {
	case "python", "javascript", "typescript", "tsx":
		return true
	}
	return false
}

// ConcreteKind maps semantic declaration categories, never AST node names or a
// generic Symbol label. Unknown categories are diagnosed by the caller.
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

func outlineQuery(entry grammars.LangEntry) string {
	query := grammars.ResolveTagsQuery(entry)
	if entry.Name == "javascript" || entry.Name == "typescript" || entry.Name == "tsx" {
		query += "\n(variable_declarator name: (identifier) @name) @definition.variable"
		if entry.Name != "javascript" {
			query += "\n(type_alias_declaration name: (type_identifier) @name) @definition.type_alias"
		}
	}
	return query
}

var definitionCapture = regexp.MustCompile(`@definition\.([a-z_]+)`)

// DeclarationKinds reads advertised capture data for the requested grammar.
// Runtime omissions and unsupported categories still have diagnostics.
func DeclarationKinds(entry grammars.LangEntry) []string {
	seen := map[string]bool{}
	for _, match := range definitionCapture.FindAllStringSubmatch(outlineQuery(entry), -1) {
		if kind := ConcreteKind(match[1]); kind != "" {
			seen[kind] = true
		}
	}
	// The outliner refines these generic captures using the declaration shape.
	query := outlineQuery(entry)
	for node, kind := range map[string]string{"enum_declaration": "Enum", "enum_item": "Enum", "record_declaration": "Record", "struct_item": "Struct", "struct_specifier": "Struct"} {
		if strings.Contains(query, "("+node) {
			seen[kind] = true
		}
	}
	if entry.Name == "python" && seen["Function"] {
		seen["Method"] = true
	}
	kinds := make([]string, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
