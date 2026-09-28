package ecmascript

import (
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// refineCallableReferences removes syntax names that the shared reference
// walker cannot distinguish from value reads in these declaration shapes.
func refineCallableReferences(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	names := map[Span]bool{}
	mark := func(n *gts.Node) {
		names[Span{Start: int(n.StartByte()), End: int(n.EndByte())}] = true
	}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(lang) {
		case "generator_function_declaration":
			if name := n.ChildByFieldName("name", lang); name != nil {
				mark(name)
			}
		case "method_definition":
			// Before the parameter list, direct property identifiers are
			// declaration syntax (name or modifier), never expression reads.
			// Computed names have their own wrapper and remain value reads.
			for i := 0; i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				switch child.Type(lang) {
				case "formal_parameters", "type_parameters":
					return
				case "property_identifier":
					mark(child)
				}
			}
		}
	})
	refs := f.References[:0]
	for _, ref := range f.References {
		if !names[ref.Span] {
			refs = append(refs, ref)
		}
	}
	f.References = refs
}
