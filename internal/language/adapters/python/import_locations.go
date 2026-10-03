package python

import (
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// The upstream import fact spans the statement. Retain each alias's range from
// the same tree without changing the statement span used by lexical binding.
func locateImportItems(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "import_statement" && n.Type(lang) != "import_from_statement" {
			return
		}
		module := n.ChildByFieldName("module_name", lang)
		for i := range f.Imports {
			imp := &f.Imports[i]
			if imp.Start != int(n.StartByte()) || len(imp.Bindings) != 1 {
				continue
			}
			wanted := imp.Path
			if imp.From != "" {
				wanted = imp.Binding
			}
			for j := 0; j < n.NamedChildCount(); j++ {
				child := n.NamedChild(j)
				if module != nil && syntax.NodeSpan(child) == syntax.NodeSpan(module) {
					continue
				}
				name, local := child, child
				if child.Type(lang) == "aliased_import" {
					name = child.ChildByFieldName("name", lang)
					local = child.ChildByFieldName("alias", lang)
				}
				if name == nil || local == nil || name.Text(f.Source) != wanted {
					continue
				}
				imp.Bindings[0].ItemSpan = syntax.NodeSpan(child)
				imp.Bindings[0].NameSpan = syntax.NodeSpan(local)
			}
		}
	})
}
