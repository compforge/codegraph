package ecmascript

import (
	"bytes"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

func attachDocumentation(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var comments []analysis.Documentation
	wrappers := map[int]int{}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(lang) {
		case "export_statement", "ambient_declaration":
			if d := n.ChildByFieldName("declaration", lang); d != nil {
				wrappers[int(d.StartByte())] = int(n.StartByte())
			} else if n.NamedChildCount() == 1 {
				wrappers[int(n.NamedChild(0).StartByte())] = int(n.StartByte())
			}
		case "lexical_declaration", "variable_declaration":
			var declaration *gts.Node
			count := 0
			for i := 0; i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child.Type(lang) == "variable_declarator" {
					declaration = child
					count++
				}
			}
			// A statement-level doc cannot identify one member of a multi-name
			// declaration. Preserve the ambiguity instead of duplicating it.
			if count == 1 {
				wrappers[int(declaration.StartByte())] = int(n.StartByte())
			}
		case "comment":
			raw := n.Text(f.Source)
			if len(raw) >= 5 && raw[:3] == "/**" {
				span := analysis.Span{Start: int(n.StartByte()), End: int(n.EndByte())}
				comments = append(comments, analysis.Documentation{Text: raw, Span: span})
			}
		}
	})
	for i := range f.Declarations {
		d := &f.Declarations[i]
		start := d.Start
		for {
			outer, ok := wrappers[start]
			if !ok || outer >= start {
				break
			}
			start = outer
		}
		for j := sort.Search(len(comments), func(j int) bool { return comments[j].End > start }) - 1; j >= 0; j-- {
			doc := comments[j]
			gap := f.Source[doc.End:start]
			if len(bytes.TrimSpace(gap)) != 0 || bytes.Count(gap, []byte("\n")) > 1 {
				break
			}
			lineStart := bytes.LastIndexByte(f.Source[:doc.Start], '\n') + 1
			if len(bytes.TrimSpace(f.Source[lineStart:doc.Start])) != 0 {
				break
			}
			d.Documentation = []analysis.Documentation{doc}
			break
		}
	}
}
