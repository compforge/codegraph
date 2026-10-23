package analysis

import "testing"

// +case:id=unsupported_shadow,desc=`A binding without a publishable declaration still shadows the enclosing name`
func TestSymbolTablePreservesUnsupportedShadow(t *testing.T) {
	table := NewSymbolTable(100)
	table.Add(Binding{Name: "value", Scope: 0, Target: 0, Span: Span{Start: 0, End: 10}})
	inner := table.AddScope(Span{Start: 20, End: 80}, 0, "function")
	table.Add(Binding{Name: "value", Scope: inner, Target: -1, Span: Span{Start: 25, End: 30}})
	got := table.Lookup("value", Span{Start: 40, End: 45})
	if len(got) != 1 || got[0].Target != -1 {
		t.Fatalf("incorrect fallback: %+v", got)
	}
	outer := table.Lookup("value", Span{Start: 90, End: 95})
	if len(outer) != 1 || outer[0].Target != 0 {
		t.Fatal(outer)
	}
	class := table.AddScope(Span{Start: 20, End: 80}, 0, "class")
	table.Add(Binding{Name: "methodOnly", Scope: class, Target: 1, Span: Span{Start: 21, End: 22}})
	table.AddScope(Span{Start: 50, End: 60}, 0, "method")
	if got := table.Lookup("methodOnly", Span{Start: 55, End: 56}); len(got) != 0 {
		t.Fatal("method lookup ignored language-selected parent", got)
	}
}
