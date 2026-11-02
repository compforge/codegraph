package analysis

import (
	"strings"

	"github.com/compforge/codegraph/pkg"
)

func ConcreteKind(kind string) pkg.NodeKind {
	switch kind {
	case "function", "method", "struct", "interface", "field", "type", "class",
		"constructor", "constant", "variable", "module", "enum", "record",
		"namespace", "property", "trait", "macro", "union":
		return pkg.NodeKind(strings.ToUpper(kind[:1]) + kind[1:])
	case "type_alias":
		return "TypeAlias"
	}
	return ""
}
