package syntax

import (
	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
)

// Modifier applications own a source identity even when their target is unknown.
// Downward ancestry handles decorators outside a declaration's grammar node.
func (x ModuleExtractor) extractDecorators(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var visit func(*gts.Node, *gts.Node)
	visit = func(n, parent *gts.Node) {
		if n.Type(lang) == "decorator" && parent != nil {
			owner := -1
			container := NodeSpan(parent)
			best := len(f.Source) + 1
			for i, d := range f.Declarations {
				if container.Start <= d.Start && container.End >= d.End && d.End-d.Start < best && d.End >= int(n.EndByte()) {
					// A wrapper's immediate declaration precedes any nested declarations.
					if owner < 0 || d.Start < f.Declarations[owner].Start || d.Start == f.Declarations[owner].Start && d.End > f.Declarations[owner].End {
						owner = i
						best = d.End - d.Start
					}
				}
			}
			if owner >= 0 {
				target := n.NamedChild(0)
				if target != nil && (target.Type(lang) == "call" || target.Type(lang) == "call_expression") {
					target = target.ChildByFieldName("function", lang)
				}
				name, receiver := x.moduleTypeName(target, lang, f.Source)
				span := NodeSpan(n)
				r := analysis.Reference{Kind: "decorates", Name: name, Receiver: receiver, Span: span, Owner: ReferenceOwner(f, span), Target: -1, Decorated: owner}
				if name == "" {
					r.Name = n.Text(f.Source)
					r.Bound = true
				} else if receiver == "" {
					bs := x.lexical.Lookup(name, span)
					if len(bs) > 0 && !(len(bs) == 1 && bs[0].Kind == "import") {
						r.Bound = true
						if len(bs) == 1 {
							r.Target = bs[0].Target
						}
					}
				}
				f.References = append(f.References, r)
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), n)
		}
	}
	visit(tree.RootNode(), nil)
}
