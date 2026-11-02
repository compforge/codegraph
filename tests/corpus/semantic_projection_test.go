package corpus_test

import (
	"fmt"
	cg "github.com/compforge/codegraph"
)

// bindingObservation adapts the graph vocabulary to the independent oracle's
// identifier-binding metric. It does not assess operation kinds; semantic kind
// contracts are tested by source fixtures in the root and semantics suites.
// Raw observed artifacts always retain their original kinds.
func bindingObservation(a observed) observed {
	out := a
	out.Relations = nil
	seen := map[string]bool{}
	add := func(r cg.Relation) {
		key := fmt.Sprintf("%s/%s/%s/%s/%d/%d", r.Source, r.Target, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte)
		if r.Kind == cg.References {
			if seen[key] {
				return
			}
			seen[key] = true
		}
		out.Relations = append(out.Relations, r)
	}
	refs := map[string][]cg.FactReference{}
	for _, f := range a.Facts {
		refs[f.Path] = f.References
	}
	for _, r := range a.Relations {
		switch r.Kind {
		case cg.Reads, cg.Writes, cg.UsesType:
			r.Kind = cg.References
			add(r)
		case cg.Calls:
			add(r)
			// The oracle inventories the callee token separately from its call
			// expression. Reuse syntax spans, never infer a target from a name.
			for _, ref := range refs[r.Location.Path] {
				at := ref.Location
				if at.StartByte >= r.Location.StartByte && at.EndByte <= r.Location.EndByte {
					for _, f := range a.Facts {
						if f.Path != at.Path {
							continue
						}
						for _, call := range f.Calls {
							if call.Location == r.Location && call.Name == ref.Name && call.Receiver == ref.Receiver {
								binding := r
								binding.Kind = cg.References
								binding.Location = at
								add(binding)
							}
						}
					}
				}
			}
		case cg.Captures: // Additional lexical-lifetime evidence, not another identifier occurrence.
		default:
			add(r)
		}
	}
	out.Report.Diagnostics = append([]cg.Diagnostic(nil), a.Report.Diagnostics...)
	for i := range out.Report.Diagnostics {
		d := &out.Report.Diagnostics[i]
		if d.Code == "unresolved_reference" {
			d.Relation = cg.References
		}
	}
	return out
}
