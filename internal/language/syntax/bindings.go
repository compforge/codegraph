package syntax

// Every use consults the same lexical index. Unknown local bindings shadow a
// declaration just as known ones do; namespace membership cannot bypass them.
func (x ModuleExtractor) hasBindingConflict(use, target Span, name string) bool {
	bindings := x.lexical.Lookup(name, use)
	for _, b := range bindings {
		if b.Span != target {
			return true
		}
	}
	return false
}
