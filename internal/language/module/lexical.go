package module

import "github.com/compforge/codegraph/internal/analysis"

// lexicalReferenceTargets is shared by identifier uses and direct calls.
// Ambiguous retained bindings are candidates; an unrepresented binding blocks
// fallback even if a same-named declaration exists elsewhere in the module.
func lexicalReferenceTargets(f analysis.Facts, r analysis.Reference) ([]Ref, analysis.Confidence) {
	if r.BindingState() == analysis.Bound {
		return []Ref{analysis.DeclarationRef(f.Path, r.Target)}, analysis.Exact
	}
	if r.BindingState() != analysis.Shadowed || r.Receiver != "" || r.Member || f.Lexical == nil {
		return nil, analysis.Exact
	}
	bindings := f.Lexical.Lookup(r.Name, r.Span)
	if len(bindings) < 2 {
		return nil, analysis.Exact
	}
	var targets []Ref
	seen := map[int]bool{}
	for _, b := range bindings {
		if b.Target < 0 {
			return nil, analysis.Exact
		}
		if !seen[b.Target] {
			targets = append(targets, analysis.DeclarationRef(f.Path, b.Target))
			seen[b.Target] = true
		}
	}
	return targets, analysis.Scoped
}
