package syntax

import (
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// CaptureSignatures joins graph declarations to header ranges from the grammar.
// Several declarations may share a single syntax node, as in Go's var a, b int.
func CaptureSignatures(f *Facts, tree *gts.Tree) {
	bySpan := make(map[Span][]int, len(f.Declarations))
	for i, d := range f.Declarations {
		switch d.Kind {
		case "function", "method", "class", "struct", "type", "interface", "namespace", "type_alias", "field", "property", "variable", "constant", "enum":
			// Some graph declarations project a single binding name (e.g. an enum member).
			if d.Span == d.NameSpan {
				f.Declarations[i].SignatureSpan = d.NameSpan
			}
			bySpan[d.Span] = append(bySpan[d.Span], i)
		}
	}
	Walk(tree.RootNode(), func(n *gts.Node) {
		span := NodeSpan(n)
		indices := bySpan[span]
		if len(indices) == 0 {
			return
		}
		header, ok := grammars.HeaderRange(n, tree.Language())
		if ok {
			for _, i := range indices {
				f.Declarations[i].SignatureSpan = Span{Start: int(header.StartByte), End: int(header.EndByte)}
			}
			delete(bySpan, span)
		}
	})
}
