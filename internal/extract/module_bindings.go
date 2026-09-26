package extract

import (
	gts "github.com/odvcencio/gotreesitter"
	"strings"
)

func moduleImportBindings(n *gts.Node, lang *gts.Language, source []byte) []ImportBinding {
	var out []ImportBinding
	span := Span{int(n.StartByte()), int(n.EndByte())}
	reExport := n.Type(lang) == "export_statement"
	walk(n, func(child *gts.Node) {
		switch child.Type(lang) {
		case "import_specifier", "export_specifier":
			name := child.ChildByFieldName("name", lang)
			alias := child.ChildByFieldName("alias", lang)
			if name != nil {
				local := name.Text(source)
				if alias != nil {
					local = alias.Text(source)
				}
				out = append(out, ImportBinding{Name: name.Text(source), Local: local, ReExport: reExport, Span: span})
			}
		case "namespace_export":
			for i := 0; i < child.NamedChildCount(); i++ {
				id := child.NamedChild(i)
				if id.Type(lang) == "identifier" {
					out = append(out, ImportBinding{Name: "*", Local: id.Text(source), Namespace: true, ReExport: true, Span: span})
				}
			}
		case "namespace_import":
			for i := 0; i < child.NamedChildCount(); i++ {
				if id := child.NamedChild(i); id.Type(lang) == "identifier" {
					out = append(out, ImportBinding{Local: id.Text(source), Namespace: true, Span: span})
				}
			}
		case "import_clause":
			for i := 0; i < child.NamedChildCount(); i++ {
				if id := child.NamedChild(i); id.Type(lang) == "identifier" {
					out = append(out, ImportBinding{Name: "default", Local: id.Text(source), Span: span})
				}
			}
		}
	})
	if reExport && len(out) == 0 && strings.Contains(n.Text(source), "*") {
		out = append(out, ImportBinding{Name: "*", Local: "*", ReExport: true, Span: span})
	}
	return out
}

func enrichModuleBindings(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	if f.Language == "python" {
		for i := range f.Imports {
			imp := &f.Imports[i]
			local := imp.Binding
			if imp.Alias != "" {
				local = imp.Alias
			}
			if imp.From == "" && imp.Alias == "" {
				local = strings.Split(imp.Path, ".")[0]
			}
			if local == "" || local == "*" {
				continue
			}
			b := ImportBinding{Local: local, Namespace: imp.From == "", Span: imp.Span}
			if len(imp.Names) == 1 {
				b.Name = imp.Names[0]
			}
			imp.Bindings = []ImportBinding{b}
		}
		return
	}
	if f.Exports == nil {
		f.Exports = map[string]string{}
	}
	walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "export_statement" || n.ChildByFieldName("source", lang) != nil {
			return
		}
		for _, field := range []string{"declaration", "value"} {
			if d := n.ChildByFieldName(field, lang); d != nil {
				for _, decl := range f.Declarations {
					if decl.Parent == -1 && int(d.StartByte()) <= decl.Start && int(d.EndByte()) >= decl.End {
						name := decl.Name
						if strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
							name = "default"
						}
						f.Exports[name] = decl.Name
					}
				}
				if d.Type(lang) == "identifier" && strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
					f.Exports["default"] = d.Text(f.Source)
				}
			}
		}
		walk(n, func(spec *gts.Node) {
			if spec.Type(lang) != "export_specifier" {
				return
			}
			name := spec.ChildByFieldName("name", lang)
			alias := spec.ChildByFieldName("alias", lang)
			if name != nil {
				public := name.Text(f.Source)
				if alias != nil {
					public = alias.Text(f.Source)
				}
				f.Exports[public] = name.Text(f.Source)
			}
		})
	})
}

func importBindingForUse(f *Facts, name, receiver string, span Span) (ImportBinding, bool) {
	local := name
	if receiver != "" {
		local = receiver
	}
	for _, imp := range f.Imports {
		for _, b := range imp.Bindings {
			if b.ReExport || b.Local != local || b.Namespace != (receiver != "") {
				continue
			}
			owner := referenceOwner(f, b.Span)
			if owner >= 0 && (f.Declarations[owner].Start > span.Start || f.Declarations[owner].End < span.End) {
				continue
			}
			return b, true
		}
	}
	return ImportBinding{}, false
}

func bindModuleUses(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	nodes := map[Span]*gts.Node{}
	var closures []Span
	walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{int(n.StartByte()), int(n.EndByte())}
		nodes[span] = n
		switch n.Type(lang) {
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, span)
		}
	})
	for i := range f.References {
		r := &f.References[i]
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
		b, ok := importBindingForUse(f, r.Name, receiver, r.Span)
		if !ok {
			continue
		}
		if n := nodes[r.Span]; n != nil && hasBindingConflict(tree, n, Declaration{Kind: "import", Span: b.Span}, b.Local) {
			r.Bound = true
			r.Target = -1
		}
	}
	for i := range f.Calls {
		c := &f.Calls[i]
		b, ok := importBindingForUse(f, c.Name, c.Receiver, c.Span)
		if !ok {
			continue
		}
		n := nodes[c.Span]
		if n == nil || hasBindingConflict(tree, n, Declaration{Kind: "import", Span: b.Span}, b.Local) {
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
