package syntax

import (
	"bytes"
	gts "github.com/odvcencio/gotreesitter"
)

// CaptureSignatures reuses grammar-owned body/value boundaries. Outliner
// provides declaration identity and ranges, but no signature span.
func CaptureSignatures(f *Facts, tree *gts.Tree) {
	declarations := make(map[Span]int, len(f.Declarations))
	for i, d := range f.Declarations {
		declarations[d.Span] = i
	}
	wrappers := map[Span]int{}
	Walk(tree.RootNode(), func(n *gts.Node) {
		if typ := n.Type(tree.Language()); typ == "export_statement" || typ == "decorated_definition" {
			for _, field := range []string{"declaration", "definition"} {
				if child := n.ChildByFieldName(field, tree.Language()); child != nil {
					wrappers[NodeSpan(child)] = int(n.StartByte())
				}
			}
		}
	})
	Walk(tree.RootNode(), func(n *gts.Node) {
		i, ok := declarations[NodeSpan(n)]
		if !ok {
			return
		}
		d := &f.Declarations[i]
		switch d.Kind {
		case "function", "method", "class", "interface", "namespace", "type_alias", "field", "property", "variable", "enum":
		default:
			return
		}
		body := n.ChildByFieldName("body", tree.Language())
		// Generic grammars only promise headers when they expose a body boundary.
		if body == nil && f.Language != "python" && f.Language != "javascript" && f.Language != "typescript" && f.Language != "tsx" {
			return
		}
		end := d.End
		if body != nil {
			end = int(body.StartByte())
		}
		if value := n.ChildByFieldName("value", tree.Language()); value != nil && (d.Kind == "variable" || d.Kind == "field" || d.Kind == "property") {
			end = int(value.StartByte())
			end = d.Start + len(bytes.TrimRight(f.Source[d.Start:end], " \t\r\n="))
		}
		// Python's header colon is part of the signature; indentation is not.
		end = d.Start + len(bytes.TrimRight(f.Source[d.Start:end], " \t\r\n"))
		start := d.Start
		if wrapper, ok := wrappers[d.Span]; ok {
			start = wrapper
		}
		d.SignatureSpan = Span{Start: start, End: end}
	})
}
