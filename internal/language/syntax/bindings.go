package syntax

import gts "github.com/odvcencio/gotreesitter"

// Every use consults the same lexical index. Unknown local bindings shadow a
// declaration just as known ones do; namespace membership cannot bypass them.
func (x ModuleExtractor) hasBindingConflict(_ *gts.Tree, use *gts.Node, target Declaration, name string) bool {
	bindings := x.lexical.Lookup(name, NodeSpan(use))
	for _, b := range bindings {
		if b.Span != target.Span {
			return true
		}
	}
	return false
}
