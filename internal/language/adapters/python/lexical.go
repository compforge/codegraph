package python

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

func (d Dialect) Lexical(f *Facts, tree *gts.Tree) *analysis.Lexicon {
	l := analysis.NewLexicon(len(f.Source))
	lang := tree.Language()
	var bind func(*gts.Node, *gts.Node, int, string)
	bind = func(n, owner *gts.Node, scope int, kind string) {
		if n == nil {
			return
		}
		switch n.Type(lang) {
		case "identifier":
			syntax.AddLexicalBinding(l, f, tree, scope, n, owner, kind)
			return
		case "attribute", "subscript":
			return
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			bind(n.NamedChild(i), owner, scope, kind)
		}
	}
	var visit func(*gts.Node, int)
	visit = func(n *gts.Node, scope int) {
		typ := n.Type(lang)
		if typ == "function_definition" || typ == "class_definition" || typ == "lambda" {
			bind(n.ChildByFieldName("name", lang), n, scope, "declaration")
			body := n.ChildByFieldName("body", lang)
			if body == nil {
				return
			}
			parent := scope
			if typ != "class_definition" && l.Scopes[parent].Kind == "class" {
				parent = l.Scopes[parent].Parent
			}
			kind := "function"
			if typ == "class_definition" {
				kind = "class"
			}
			inner := l.AddScope(syntax.NodeSpan(body), parent, kind)
			if params := n.ChildByFieldName("parameters", lang); params != nil {
				names, expressions, _ := d.Parameters(params, lang)
				for _, id := range names {
					owner := id
					for i := 0; i < params.NamedChildCount(); i++ {
						c := params.NamedChild(i)
						if syntax.NodeContains(c, id) {
							owner = c
							break
						}
					}
					bind(id, owner, inner, "parameter")
				}
				for _, expr := range expressions {
					visit(expr.Node, scope)
				}
			}
			for i := 0; i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c == body {
					visit(c, inner)
				} else if c != n.ChildByFieldName("parameters", lang) && c != n.ChildByFieldName("name", lang) {
					visit(c, scope)
				}
			}
			return
		}
		switch typ {
		case "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
			scope = l.AddScope(syntax.NodeSpan(n), scope, "comprehension")
		case "assignment", "augmented_assignment", "named_expression":
			bind(n.ChildByFieldName("left", lang), n, scope, "local")
			bind(n.ChildByFieldName("name", lang), n, scope, "local")
		case "for_statement", "for_in_clause":
			bind(n.ChildByFieldName("left", lang), n, scope, "local")
		case "as_pattern":
			bind(n.ChildByFieldName("alias", lang), n, scope, "local")
		case "global_statement", "nonlocal_statement", "delete_statement":
			// Redirection/deletion needs execution evidence. Retain a shadowing binding
			// instead of inventing a unique target in the surrounding module.
			for i := 0; i < n.NamedChildCount(); i++ {
				bind(n.NamedChild(i), n, scope, "redirect")
			}
		case "import_statement", "import_from_statement":
			return
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), scope)
		}
	}
	visit(tree.RootNode(), 0)
	syntax.AddLexicalImports(l, f)
	return l
}
