package syntax

import (
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/pkg"
	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) extractModuleReferences(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var visit func(*gts.Node, *gts.Node, pkg.RelationKind)
	visit = func(n, parent *gts.Node, role pkg.RelationKind) {
		typ := n.Type(lang)
		if typ == "comment" || typ == "string" || typ == "import_statement" || typ == "import_from_statement" {
			return
		}
		if _, expressions, handled := x.Dialect.Parameters(n, lang, x.signatures); handled {
			for _, expr := range expressions {
				visit(expr.Node, expr.Parameter, moduleUseRole(expr.Node, expr.Parameter, lang, role))
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
						case "function_definition", "function_declaration", "class_definition", "class_declaration", "type_alias_declaration", "interface_declaration", "method_definition", "variable_declarator", "assignment", "assignment_expression", "augmented_assignment", "augmented_assignment_expression", "pair", "enum_assignment", "enum_declaration", "method_signature", "property_signature", "field_definition", "public_field_definition", "internal_module", "export_specifier", "typed_parameter", "default_parameter", "required_parameter", "optional_parameter", "arrow_function":
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
			if (role == pkg.Writes || role == analysis.ReadWriteUse) && !x.lexical.IsSyntax(NodeSpan(n)) {
				binding = false
			}
			if !binding {
				span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
				ref := Reference{Kind: role, Member: member, Name: n.Text(f.Source), Receiver: receiver, Span: span, Owner: ReferenceOwner(f, span), Target: -1}
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
			child := n.NamedChild(i)
			visit(child, n, moduleUseRole(child, n, lang, role))
		}
	}
	visit(tree.RootNode(), nil, analysis.NameUse)
	sort.Slice(f.References, func(i, j int) bool { return f.References[i].Start < f.References[j].Start })
}

func moduleUseRole(n, parent *gts.Node, lang *gts.Language, inherited pkg.RelationKind) pkg.RelationKind {
	if parent == nil {
		return inherited
	}
	field := func(name string) bool {
		c := parent.ChildByFieldName(name, lang)
		return c != nil && NodeSpan(c) == NodeSpan(n)
	}
	typ := parent.Type(lang)
	switch typ {
	case "call", "call_expression", "new_expression":
		if field("function") || field("constructor") {
			return analysis.CallTargetUse
		}
		return analysis.NameUse
	case "attribute", "member_expression":
		if field("object") {
			return analysis.NameUse
		}
	case "subscript", "subscript_expression":
		if inherited == pkg.UsesType {
			return inherited
		}
		return analysis.NameUse
	case "assignment", "assignment_expression":
		if field("left") {
			return pkg.Writes
		}
		return analysis.NameUse
	case "augmented_assignment", "augmented_assignment_expression":
		if field("left") {
			return analysis.ReadWriteUse
		}
		return analysis.NameUse
	case "update_expression":
		return analysis.ReadWriteUse
	case "type_annotation", "generic_type", "type_arguments", "type_parameters", "union_type", "intersection_type", "extends_type_clause", "implements_clause":
		return pkg.UsesType
	}
	if field("type") || field("return_type") {
		return pkg.UsesType
	}
	if n.Type(lang) == "type_identifier" {
		return pkg.UsesType
	}
	return inherited
}
