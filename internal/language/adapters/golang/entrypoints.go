package golang

import gts "github.com/odvcencio/gotreesitter"

// +why=Reuse the parsed syntax tree for native entry recognition; filtering is consumer policy and must not reconstruct language rules.
func markGoEntrypoints(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	entries := map[int]bool{}
	root := tree.RootNode()
	// Go runtime entries are top-level functions with no type parameters,
	// parameters or results. Comments are named nodes, not parameters.
	for i := 0; i < root.NamedChildCount(); i++ {
		n := root.NamedChild(i)
		if n.Type(lang) != "function_declaration" {
			continue
		}
		name := n.ChildByFieldName("name", lang)
		if name == nil || (name.Text(f.Source) != "init" && (name.Text(f.Source) != "main" || f.Package != "main")) {
			continue
		}
		params := n.ChildByFieldName("parameters", lang)
		if params == nil || n.ChildByFieldName("type_parameters", lang) != nil || n.ChildByFieldName("result", lang) != nil {
			continue
		}
		empty := true
		for j := 0; j < params.NamedChildCount(); j++ {
			if params.NamedChild(j).Type(lang) != "comment" {
				empty = false
				break
			}
		}
		if empty {
			entries[int(n.StartByte())] = true
		}
	}
	for i := range f.Declarations {
		f.Declarations[i].Entrypoint = entries[f.Declarations[i].Start]
	}
}
