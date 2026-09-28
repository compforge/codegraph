package analysis

import (
	"context"
	"reflect"
	"testing"
)

func TestMethodDerivationKeepsWeakestPremiseAndStrongestRoute(t *testing.T) {
	f := Facts{Path: "a", Declarations: []Declaration{{Name: "run", Kind: "method"}}}
	x := NewIndex(map[string]Facts{"a": f})
	method := DeclarationRef("a", 0)
	a, b, c, d := SyntheticRef("a"), SyntheticRef("b"), SyntheticRef("c"), SyntheticRef("d")
	x.RegisterEntities(Organization{Entities: []Entity{{Ref: method, Name: "run"}}})
	x.Add(Edge{Source: d, Target: method, Kind: "contains", Confidence: Exact, Basis: "member"})
	// Two routes converge; the stronger one is deliberately visited second.
	for _, e := range []Edge{
		{Source: a, Target: b, Kind: "extends", Confidence: Exact},
		{Source: b, Target: d, Kind: "extends", Confidence: NameOnly},
		{Source: a, Target: c, Kind: "extends", Confidence: Scoped},
		{Source: c, Target: d, Kind: "extends", Confidence: Exact},
		{Source: d, Target: a, Kind: "extends", Confidence: Exact},
	} {
		x.Add(e)
	}
	m := NewMethodIndex(x)
	lookup := func(roots []BindingTarget) []MethodTarget {
		t.Helper()
		got, err := m.Lookup(context.Background(), roots, "run", func(Ref) bool { return true }, 10)
		if err != nil || len(got) != 1 || !got[0].Inherited || got[0].Ref != method {
			t.Fatalf("%+v %v", got, err)
		}
		return got
	}
	first := lookup([]BindingTarget{{a, Exact}})
	if first[0].Confidence != Scoped {
		t.Fatal(first)
	}
	m.Bases[a][0], m.Bases[a][1] = m.Bases[a][1], m.Bases[a][0]
	if got := lookup([]BindingTarget{{a, Exact}}); !reflect.DeepEqual(first, got) {
		t.Fatal(first, got)
	}
	if got := lookup([]BindingTarget{{a, Heuristic}}); got[0].Confidence != Heuristic {
		t.Fatal(got)
	}
}
