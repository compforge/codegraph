package syntax

import (
	"sort"

	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) extractModuleReferences(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var visit func(*gts.Node, *gts.Node)
	visit = func(n, parent *gts.Node) {
		typ := n.Type(lang)
		if typ == "comment" || typ == "string" || typ == "import_statement" || typ == "import_from_statement" {
			return
		}
		if _, expressions, handled := x.Dialect.Parameters(n, lang); handled {
			for _, expr := range expressions {
				visit(expr.Node, expr.Parameter)
			}
			return
		}

		if typ == "identifier" || typ == "property_identifier" || typ == "type_identifier" || typ == "shorthand_property_identifier" {
			binding := x.lexical.IsSyntax(NodeSpan(n))
			receiver := ""
			member := false
			if parent != nil {
				pt := parent.Type(lang)
				if pt == "pair_pattern" || pt == "pair" {
					if key := parent.ChildByFieldName("key", lang); key != nil && NodeSpan(key) == NodeSpan(n) {
						member = true
					}
				}
				for _, field := range []string{"name", "left", "parameter", "pattern", "alias"} {
					if child := parent.ChildByFieldName(field, lang); child != nil && child.StartByte() <= n.StartByte() && child.EndByte() >= n.EndByte() {
						switch pt {
						case "function_definition", "function_declaration", "class_definition", "class_declaration", "type_alias_declaration", "interface_declaration", "method_definition", "variable_declarator", "assignment", "assignment_expression", "augmented_assignment", "augmented_assignment_expression", "pair", "enum_assignment", "enum_declaration", "method_signature", "property_signature", "export_specifier", "typed_parameter", "default_parameter", "required_parameter", "optional_parameter", "arrow_function":
							binding = true
						}
					}
				}
				switch pt {
				case "parameters", "formal_parameters", "lambda_parameters", "enum_body":
					binding = true
				case "attribute", "member_expression":
					for _, field := range []string{"attribute", "property"} {
						if child := parent.ChildByFieldName(field, lang); child != nil && child.StartByte() == n.StartByte() {
							for _, owner := range []string{"object"} {
								if recv := parent.ChildByFieldName(owner, lang); recv != nil {
									receiver = recv.Text(f.Source)
								}
							}
						}
					}
				}
			}
			if !binding {
				span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
				ref := Reference{Member: member, Name: n.Text(f.Source), Receiver: receiver, Span: span, Owner: ReferenceOwner(f, span), Target: -1}
				if receiver == "" && !member {
					bindings := x.lexical.Lookup(ref.Name, span)
					if len(bindings) > 0 {
						imported := len(bindings) == 1 && bindings[0].Kind == "import"
						if !imported {
							ref.Bound = true
							if len(bindings) == 1 {
								ref.Target = bindings[0].Target
							}
						}
					}
				}
				f.References = append(f.References, ref)
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), n)
		}
	}
	visit(tree.RootNode(), nil)
	sort.Slice(f.References, func(i, j int) bool { return f.References[i].Start < f.References[j].Start })
}
