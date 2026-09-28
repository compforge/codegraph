package syntax

import (
	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) importBindingForUse(f *Facts, name, receiver string, span Span) (ImportBinding, bool) {
	local := name
	if receiver != "" {
		local = receiver
	}
	for _, imp := range f.Imports {
		for _, b := range imp.Bindings {
			if b.ReExport || b.Local != local || b.Namespace != (receiver != "") {
				continue
			}
			visible := x.lexical.Lookup(local, span)
			if len(visible) != 1 || visible[0].Kind != "import" || visible[0].Span != b.Span {
				continue
			}
			return b, true
		}
	}
	return ImportBinding{}, false
}

func (x ModuleExtractor) bindModuleUses(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	nodes := map[Span]*gts.Node{}
	var closures []Span
	Walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
		nodes[span] = n
		switch n.Type(lang) {
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, x.moduleClosureSpan(n, lang))
		}
	})
	for i := range f.References {
		r := &f.References[i]
		if r.Member && r.Receiver == "" {
			r.Bound = true
			r.Target = -1
			continue
		}
		receiver := r.Receiver
		if receiver == "" {
			for _, imp := range f.Imports {
				for _, binding := range imp.Bindings {
					if binding.Namespace && binding.Local == r.Name {
						receiver = r.Name
					}
				}
			}
		}
		if receiver != "" {
			visible := x.lexical.Lookup(receiver, r.Span)
			if len(visible) > 0 && (len(visible) != 1 || visible[0].Kind != "import") {
				r.Bound = true
				r.Target = -1
				continue
			}
		}
		b, ok := x.importBindingForUse(f, r.Name, receiver, r.Span)
		if !ok {
			continue
		}
		if n := nodes[r.Span]; n != nil && x.hasBindingConflict(tree, n, Declaration{Kind: "import", Span: b.Span}, b.Local) {
			r.Bound = true
			r.Target = -1
		}
	}
	for i := range f.Calls {
		c := &f.Calls[i]
		b, ok := x.importBindingForUse(f, c.Name, c.Receiver, c.Span)
		if !ok {
			continue
		}
		n := nodes[c.Span]
		if n == nil || x.hasBindingConflict(tree, n, Declaration{Kind: "import", Span: b.Span}, b.Local) {
			continue
		}
		closure := false
		for _, span := range closures {
			if span.Start <= c.Start && span.End >= c.End {
				closure = true
			}
		}
		if !closure {
			c.Imported = true
			c.Blocked = false
		}
	}
}
