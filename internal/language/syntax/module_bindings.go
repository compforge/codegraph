package syntax

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

func (x ModuleExtractor) bindModuleReferences(f *Facts) {
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
		if x.hasBindingConflict(r.Span, b.Span, b.Local) {
			r.Bound = true
			r.Target = -1
		}
	}
}
