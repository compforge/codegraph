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
		result := n.ChildByFieldName("result", lang)
		if n.ChildByFieldName("type_parameters", lang) != nil || !emptyGoParameterList(params, lang) || result != nil && !emptyGoParameterList(result, lang) {
			continue
		}
		entries[int(n.StartByte())] = true
	}
	for i := range f.Declarations {
		f.Declarations[i].Entrypoint = entries[f.Declarations[i].Start]
	}
}

// Empty result lists, including comments, have the same signature as no result.
func emptyGoParameterList(n *gts.Node, lang *gts.Language) bool {
	if n == nil || n.Type(lang) != "parameter_list" {
		return false
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		if n.NamedChild(i).Type(lang) != "comment" {
			return false
		}
	}
	return true
}
