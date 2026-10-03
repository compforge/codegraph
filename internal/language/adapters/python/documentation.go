package python

import (
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// Attach by syntax occurrence, not bare name: methods and nested functions can
// share names, and decorators can change the outline's declaration span.
func attachDocumentation(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	docs := map[Span]analysis.Documentation{}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		definition := n
		if n.Type(lang) == "decorated_definition" {
			definition = n.ChildByFieldName("definition", lang)
		}
		if definition == nil || (definition.Type(lang) != "function_definition" && definition.Type(lang) != "class_definition") {
			return
		}
		body := definition.ChildByFieldName("body", lang)
		if body == nil {
			return
		}
		for i := 0; i < body.NamedChildCount(); i++ {
			first := body.NamedChild(i)
			if first.Type(lang) == "comment" {
				continue
			}
			// Only the first statement can be a docstring. Do not accidentally
			// promote a string from a later expression or a nested definition.
			// gotreesitter may expose the expression directly when the grammar
			// elides the expression_statement wrapper.
			value := first
			if first.Type(lang) == "expression_statement" {
				if first.NamedChildCount() != 1 {
					return
				}
				value = first.NamedChild(0)
			}
			if !plainString(value, lang, f.Source) {
				return
			}
			span := Span{Start: int(value.StartByte()), End: int(value.EndByte())}
			docs[Span{Start: int(n.StartByte()), End: int(n.EndByte())}] = analysis.Documentation{Text: value.Text(f.Source), Span: span}
			return
		}
	})
	for i := range f.Declarations {
		d := &f.Declarations[i]
		if doc, ok := docs[d.Span]; ok {
			d.Documentation = []analysis.Documentation{doc}
		}
	}
}

func plainString(n *gts.Node, lang *gts.Language, source []byte) bool {
	switch n.Type(lang) {
	case "string":
		raw := n.Text(source)
		quote := strings.IndexAny(raw, "\"'")
		if quote < 0 {
			return false
		}
		switch strings.ToLower(raw[:quote]) {
		case "", "r", "u":
			return true
		}
	case "concatenated_string", "parenthesized_expression":
		stringsFound := 0
		for i := 0; i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child.Type(lang) == "comment" {
				continue
			}
			if !plainString(child, lang, source) {
				return false
			}
			stringsFound++
		}
		return stringsFound > 0
	}
	return false
}
