package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

func (Dialect) Lexical(f *Facts, tree *gts.Tree) *analysis.Lexicon {
	l := analysis.NewLexicon(len(f.Source))
	lang := tree.Language()
	var pattern func(*gts.Node, *gts.Node, int, string)
	pattern = func(n, owner *gts.Node, scope int, kind string) {
		if n == nil {
			return
		}
		switch n.Type(lang) {
		case "identifier", "type_identifier", "shorthand_property_identifier_pattern":
			syntax.AddLexicalBinding(l, f, tree, scope, n, owner, kind)
			return
		case "pair_pattern":
			pattern(n.ChildByFieldName("value", lang), owner, scope, kind)
			return
		case "assignment_pattern":
			pattern(n.ChildByFieldName("left", lang), owner, scope, kind)
			return
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			pattern(n.NamedChild(i), owner, scope, kind)
		}
	}
	var visit func(*gts.Node, int, int)
	visit = func(n *gts.Node, scope, fn int) {
		typ := n.Type(lang)
		switch typ {
		case "function_declaration", "generator_function_declaration", "class_declaration", "interface_declaration", "enum_declaration", "type_alias_declaration", "internal_module":
			pattern(n.ChildByFieldName("name", lang), n, scope, "declaration")
		}
		switch typ {
		case "function_declaration", "generator_function_declaration", "function_expression", "generator_function", "arrow_function", "method_definition":
			scope = l.AddScope(syntax.NodeSpan(n), scope, "function")
			fn = scope
			if typ == "function_expression" || typ == "generator_function" {
				pattern(n.ChildByFieldName("name", lang), n, scope, "declaration")
			}
			pattern(n.ChildByFieldName("parameter", lang), n, scope, "parameter")
		case "internal_module":
			scope = l.AddScope(syntax.NodeSpan(n), scope, "namespace")
			fn = scope
		case "class_declaration", "class":
			scope = l.AddScope(syntax.NodeSpan(n), scope, "class")
		case "statement_block", "for_in_statement", "for_statement", "catch_clause", "switch_body":
			scope = l.AddScope(syntax.NodeSpan(n), scope, "block")
		}
		switch typ {
		case "variable_declaration", "lexical_declaration":
			// var binds to the nearest function, including declarations inside loops.
			target := scope
			if typ == "variable_declaration" {
				target = fn
			}
			for i := 0; i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c.Type(lang) == "variable_declarator" {
					pattern(c.ChildByFieldName("name", lang), c, target, "local")
				}
			}
		case "formal_parameters":
			for i := 0; i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				name := c
				if c.Type(lang) == "required_parameter" || c.Type(lang) == "optional_parameter" {
					name = c.ChildByFieldName("pattern", lang)
					if name == nil {
						name = c.ChildByFieldName("name", lang)
					}
				}
				pattern(name, c, scope, "parameter")
			}
		case "for_in_statement":
			if kind := n.ChildByFieldName("kind", lang); kind != nil {
				target := scope
				if kind.Text(f.Source) == "var" {
					target = fn
				}
				name := n.ChildByFieldName("left", lang)
				pattern(name, name, target, "local")
			}
		case "catch_clause":
			name := n.ChildByFieldName("parameter", lang)
			if name != nil {
				pattern(name, name, scope, "parameter")
				if annotation := n.ChildByFieldName("type", lang); annotation != nil {
					for i := range f.Declarations {
						d := &f.Declarations[i]
						if d.Span == syntax.NodeSpan(name) {
							d.End = int(annotation.EndByte())
						}
					}
				}
			}
		}
		if typ == "method_definition" {
			for i := 0; i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c.Type(lang) == "formal_parameters" || c.Type(lang) == "type_parameters" {
					break
				}
				if c.Type(lang) == "property_identifier" {
					l.MarkSyntax(syntax.NodeSpan(c))
				}
			}
		}
		if typ == "import_statement" {
			return
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), scope, fn)
		}
	}
	visit(tree.RootNode(), 0, 0)
	syntax.AddLexicalImports(l, f)
	return l
}
