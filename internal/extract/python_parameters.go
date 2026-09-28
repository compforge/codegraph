package extract

import gts "github.com/odvcencio/gotreesitter"

// The Python grammar puts annotations/defaults beside the binding pattern.
// Looking for identifiers anywhere under parameters confuses reads with writes.
func pythonParameterName(parameter *gts.Node, lang *gts.Language) *gts.Node {
	n := parameter
	switch n.Type(lang) {
	case "typed_parameter":
		n = n.NamedChild(0)
	case "default_parameter", "typed_default_parameter":
		n = n.ChildByFieldName("name", lang)
	}
	if n == nil {
		return nil
	}
	if n.Type(lang) == "list_splat_pattern" || n.Type(lang) == "dictionary_splat_pattern" {
		n = n.NamedChild(0)
	}
	if n != nil && n.Type(lang) == "identifier" {
		return n
	}
	return nil
}

func visitPythonParameterExpressions(parameter *gts.Node, lang *gts.Language, visit func(*gts.Node)) {
	for _, field := range []string{"type", "value"} {
		if expr := parameter.ChildByFieldName(field, lang); expr != nil {
			visit(expr)
		}
	}
}

func nodeContains(outer, inner *gts.Node) bool {
	return outer != nil && outer.StartByte() <= inner.StartByte() && outer.EndByte() >= inner.EndByte()
}
