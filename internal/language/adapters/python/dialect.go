package python

import (
	"strings"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

type Dialect struct{}

func (Dialect) Enrich(f *Facts, tree *gts.Tree) {
	attachDocumentation(f, tree)
	for i := range f.Imports {
		imp := &f.Imports[i]
		local := imp.Binding
		if imp.Alias != "" {
			local = imp.Alias
		}
		if imp.From == "" && imp.Alias == "" {
			local = strings.Split(imp.Path, ".")[0]
		}
		if local == "" || local == "*" {
			continue
		}
		b := ImportBinding{Local: local, Namespace: imp.From == "", Span: imp.Span}
		if len(imp.Names) == 1 {
			b.Name = imp.Names[0]
		}
		imp.Bindings = []ImportBinding{b}
	}
}
func (Dialect) Parameters(n *gts.Node, lang *gts.Language) (names []*gts.Node, expressions []syntax.ParameterExpression, handled bool) {
	if n.Type(lang) != "parameters" && n.Type(lang) != "lambda_parameters" {
		return
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		p := n.NamedChild(i)
		if id := pythonParameterName(p, lang); id != nil {
			names = append(names, id)
		}
		visitPythonParameterExpressions(p, lang, func(e *gts.Node) {
			expressions = append(expressions, syntax.ParameterExpression{Node: e, Parameter: p})
		})
	}
	return names, expressions, true
}
func (Dialect) Self(f *Facts, tree *gts.Tree, owner int, receiver string) bool {
	lang := tree.Language()
	self := receiver == "this"
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		if int(n.StartByte()) != f.Declarations[owner].Start {
			return
		}
		if params := n.ChildByFieldName("parameters", lang); params != nil && params.NamedChildCount() > 0 {
			self = params.NamedChild(0).Text(f.Source) == receiver
		}
	})
	return self
}
func (Dialect) Bases(n *gts.Node, lang *gts.Language, add func(*gts.Node, string)) {
	if bases := n.ChildByFieldName("superclasses", lang); bases != nil {
		for i := 0; i < bases.NamedChildCount(); i++ {
			base := bases.NamedChild(i)
			if base.Type(lang) != "keyword_argument" {
				add(base, "extends")
			}
		}
	}
}
func (Dialect) Closure(n *gts.Node, lang *gts.Language) Span {
	if n.Type(lang) == "lambda" {
		n = n.ChildByFieldName("body", lang)
	}
	return Span{Start: int(n.StartByte()), End: int(n.EndByte())}
}
