package syntax

import (
	"bytes"
	gts "github.com/odvcencio/gotreesitter"
)

// CaptureSignatures preserves declaration-header text using grammar body/value
// boundaries; structured signature facts do not provide the complete header span.
func CaptureSignatures(f *Facts, tree *gts.Tree) {
	declarations := make(map[Span][]int, len(f.Declarations))
	for i, d := range f.Declarations {
		declarations[d.Span] = append(declarations[d.Span], i)
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
		indices, ok := declarations[NodeSpan(n)]
		if !ok {
			return
		}
		for _, i := range indices {
			d := &f.Declarations[i]
			switch d.Kind {
			case "function", "method", "class", "struct", "type", "interface", "namespace", "type_alias", "field", "property", "variable", "constant", "enum":
			default:
				return
			}
			body := n.ChildByFieldName("body", tree.Language())
			// Generic grammars only promise headers when they expose a body boundary.
			if body == nil && f.Language != "go" && f.Language != "python" && f.Language != "javascript" && f.Language != "typescript" && f.Language != "tsx" {
				return
			}
			// Go type declarations expose their implementation through the type field.
			if f.Language == "go" && (d.Kind == "struct" || d.Kind == "interface" || d.Kind == "type_alias") {
				typ := n.ChildByFieldName("type", tree.Language())
				if typ != nil {
					for j := 0; j < typ.ChildCount(); j++ {
						child := typ.Child(j)
						if child.Type(tree.Language()) == "field_declaration_list" || child.Type(tree.Language()) == "{" {
							body = child
							break
						}
					}
				}
			}
			end := d.End
			if body != nil {
				end = int(body.StartByte())
			}
			if value := n.ChildByFieldName("value", tree.Language()); value != nil && (d.Kind == "variable" || d.Kind == "constant" || d.Kind == "field" || d.Kind == "property") {
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
		}
	})
}
