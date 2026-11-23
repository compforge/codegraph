package python

import (
	"strings"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Dialect struct{}

func (Dialect) Enrich(f *Facts, tree *gts.Tree, facts gts.FactSet) {
	attachDocumentation(f, tree)
	// Both upstream views enumerate the items of a statement in source order.
	var statement Span
	var items []grammars.ImportItem
	for i := range f.Imports {
		imp := &f.Imports[i]
		// A bare relative prefix is still a from-import. Upstream leaves From
		// empty for `from .. import name`; retain the package anchor so this is
		// not mistaken for a namespace import of `name` itself.
		if imp.From == "" && imp.Relative > 0 {
			imp.From = strings.Repeat(".", imp.Relative)
		}
		if imp.Span != statement {
			statement = imp.Span
			n := syntax.NodeAtExactRange(tree.RootNode(), uint32(imp.Start), uint32(imp.End))
			items = grammars.ImportItems(n, tree.Language())
		}
		var item grammars.ImportItem
		if len(items) > 0 {
			item, items = items[0], items[1:]
		}
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
		if item.BindingRange.EndByte > item.BindingRange.StartByte && string(f.Source[item.BindingRange.StartByte:item.BindingRange.EndByte]) == local {
			b.ItemSpan = syntax.ByteSpan(item.Range)
			b.NameSpan = syntax.ByteSpan(item.BindingRange)
		}
		imp.Bindings = []ImportBinding{b}
	}
}
func (Dialect) Parameters(n *gts.Node, lang *gts.Language, signatures []gts.SignatureFact) (names []*gts.Node, expressions []syntax.ParameterExpression, handled bool) {
	if n.Type(lang) != "parameters" && n.Type(lang) != "lambda_parameters" {
		return
	}
	if parameters, ok := syntax.SignatureParameters(signatures, n, lang); ok && n.Type(lang) != "lambda_parameters" {
		syntax.Walk(n, func(id *gts.Node) {
			if id.Type(lang) != "identifier" {
				return
			}
			for _, p := range parameters {
				if int(id.StartByte()) >= int(p.NameStartByte) && int(id.EndByte()) <= int(p.NameEndByte) {
					names = append(names, id)
					break
				}
			}
		})
	} else if n.Type(lang) == "lambda_parameters" {
		// Unbound lambdas have no upstream signature facts.
		for i := 0; i < n.NamedChildCount(); i++ {
			if id := pythonLambdaParameterName(n.NamedChild(i), lang); id != nil {
				names = append(names, id)
			}
		}
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		p := n.NamedChild(i)
		visitPythonParameterExpressions(p, lang, func(e *gts.Node) {
			expressions = append(expressions, syntax.ParameterExpression{Node: e, Parameter: p})
		})
	}
	return names, expressions, true
}
func (Dialect) Self(f *Facts, tree *gts.Tree, owner int, receiver string, signatures []gts.SignatureFact) bool {
	d := f.Declarations[owner]
	for _, s := range signatures {
		if int(s.NameStartByte) == d.NameSpan.Start && len(s.Parameters) > 0 {
			return s.Parameters[0].Name == receiver
		}
	}
	return false
}
func (Dialect) Closure(n *gts.Node, lang *gts.Language) Span {
	if n.Type(lang) == "lambda" {
		n = n.ChildByFieldName("body", lang)
	}
	return Span{Start: int(n.StartByte()), End: int(n.EndByte())}
}
