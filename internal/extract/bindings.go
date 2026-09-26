package extract

import (
	"strings"

	gts "github.com/odvcencio/gotreesitter"
)

// hasBindingConflict checks lexical ancestors only. Unrelated function bodies
// cannot shadow a module binding. Unsupported binding forms fail closed.
func hasBindingConflict(tree *gts.Tree, call *gts.Node, target Declaration, name string) bool {
	lang, source := tree.Language(), tree.Source()
	conflict := false
	var visit func(*gts.Node, []*gts.Node)
	visit = func(n *gts.Node, ancestors []*gts.Node) {
		if conflict {
			return
		}
		typ := n.Type(lang)
		if target.Kind == "import" && (typ == "import_statement" || typ == "import_from_statement") {
			return
		}
		if target.Kind == "import" && int(n.StartByte()) >= target.Start && int(n.EndByte()) <= target.End {
			return
		}
		contains := n.StartByte() <= call.StartByte() && n.EndByte() >= call.EndByte()
		scope := typ == "function_definition" || typ == "function_declaration" || typ == "method_definition" ||
			typ == "class_definition" || typ == "class_declaration" || typ == "arrow_function" ||
			typ == "function_expression" || typ == "lambda" || strings.HasPrefix(typ, "generator_function")
		if scope && !contains {
			id := n.ChildByFieldName("name", lang)
			if id != nil && id.Text(source) == name &&
				(n.StartByte() != uint32(target.Start) || n.EndByte() != uint32(target.End)) {
				conflict = true
			}
			return
		}
		if typ == "identifier" && n.Text(source) == name {
			for i := len(ancestors) - 1; i >= 0; i-- {
				p := ancestors[i]
				switch p.Type(lang) {
				case "function_definition", "function_declaration":
					if id := p.ChildByFieldName("name", lang); id != nil && id.StartByte() == n.StartByte() {
						if p.StartByte() != uint32(target.Start) || p.EndByte() != uint32(target.End) {
							conflict = true
						}
					}
					return
				case "arrow_function":
					if parameter := p.ChildByFieldName("parameter", lang); parameter != nil && parameter.StartByte() == n.StartByte() {
						conflict = true
					}
					return
				case "parameters", "formal_parameters", "lambda_parameters", "import_statement", "import_from_statement",
					"global_statement", "nonlocal_statement", "named_expression", "for_in_statement", "for_statement",
					"with_item", "except_clause", "delete_statement", "catch_clause", "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
					conflict = true
					return
				case "variable_declarator", "assignment", "augmented_assignment", "assignment_expression", "augmented_assignment_expression":
					for _, field := range []string{"name", "left"} {
						if binding := p.ChildByFieldName(field, lang); binding != nil && binding.StartByte() <= n.StartByte() && binding.EndByte() >= n.EndByte() {
							conflict = true
						}
					}
					return
				case "call", "call_expression", "expression_statement", "return_statement", "block", "statement_block":
					return
				}
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), append(ancestors, n))
		}
	}
	visit(tree.RootNode(), nil)
	return conflict
}
