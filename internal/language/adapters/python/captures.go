package python

import "github.com/compforge/codegraph/internal/analysis"

// retainCapturedLocals promotes only lexical storage needed by a nested callable.
// Ordinary unsupported locals remain binding evidence; a capture needs a stable
// target so admission can retain its dependency bridge.
// +spec=Promotion preserves existing declaration indexes and lexical shadowing.
// +spec=Captured locals from the same assignment remain siblings regardless of reference order.
// +why=Newly promoted storage is not a lexical container for later promotions.
// +case=component://tests/semantics/python_capture_parent_test.go
// +link=component://docs/language-pipeline.md
func retainCapturedLocals(f *Facts) {
	if f.Lexical == nil {
		return
	}
	owners := *f // Keep the original declaration slice length while promotions append to f.
	slots := map[analysis.Span]int{}
	for i, b := range f.Lexical.Bindings {
		slots[b.NameSite] = i
	}
	for _, r := range f.References {
		if !r.Bound || r.Target >= 0 || r.Receiver != "" || r.Member {
			continue
		}
		bindings := f.Lexical.Lookup(r.Name, r.Span)
		if len(bindings) != 1 {
			continue
		}
		b := bindings[0]
		if b.Kind != "local" || b.Target >= 0 || f.Lexical.Scopes[b.Scope].Kind != "function" {
			continue
		}
		scope := f.Lexical.ScopeAt(r.Span)
		crossed := false
		for scope >= 0 && scope != b.Scope {
			if f.Lexical.Scopes[scope].Kind == "function" {
				crossed = true
			}
			scope = f.Lexical.Scopes[scope].Parent
		}
		if scope != b.Scope || !crossed {
			continue
		}
		parent := analysis.EnclosingDeclaration(owners, b.Span)
		if parent < 0 {
			continue
		}
		target := len(f.Declarations)
		f.Declarations = append(f.Declarations, analysis.Declaration{Name: b.Name, QualifiedName: f.Declarations[parent].QualifiedName + "." + b.Name, Kind: "variable", Parent: parent, Span: b.Span, NameSpan: b.NameSite})
		f.Lexical.Bindings[slots[b.NameSite]].Target = target
	}
	for i := range f.References {
		r := &f.References[i]
		if !r.Bound || r.Target >= 0 || r.Receiver != "" || r.Member {
			continue
		}
		bindings := f.Lexical.Lookup(r.Name, r.Span)
		if len(bindings) == 1 && bindings[0].Target >= 0 {
			r.Target = bindings[0].Target
		}
	}
}
