package analysis

import "strings"

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
