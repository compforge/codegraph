package ecmascript

import (
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// refineControlBindings keeps declaration ranges separate from the enclosing
// control statement. Catch annotations belong to the binding; its body does not.
func refineControlBindings(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	bindings := map[Span]Span{}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		var name *gts.Node
		switch n.Type(lang) {
		case "for_in_statement":
			if n.ChildByFieldName("kind", lang) == nil {
				return
			}
			name = n.ChildByFieldName("left", lang)
		case "catch_clause":
			name = n.ChildByFieldName("parameter", lang)
		}
		if name == nil || name.Type(lang) != "identifier" {
			return
		}
		at := Span{Start: int(name.StartByte()), End: int(name.EndByte())}
		span := at
		if annotation := n.ChildByFieldName("type", lang); annotation != nil {
			span.End = int(annotation.EndByte())
		}
		bindings[at] = span
	})
	for i := range f.Declarations {
		if span, ok := bindings[f.Declarations[i].Span]; ok {
			f.Declarations[i].Span = span
		}
	}
	// The shared syntax walker sees bare identifiers in these grammar nodes.
	// A binding name is a declaration, not a read of its own value.
	refs := f.References[:0]
	for _, ref := range f.References {
		if _, binding := bindings[ref.Span]; !binding {
			refs = append(refs, ref)
		}
	}
	f.References = refs
}
