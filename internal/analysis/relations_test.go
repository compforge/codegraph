package analysis

import (
	"reflect"
	"testing"
)

func TestRelationOccurrenceAndEvidence(t *testing.T) {
	source, target := DocumentRef("a"), DocumentRef("b")
	edge := Edge{Source: source, Target: target, Kind: "calls", Path: "a", Span: Span{1, 3}, Confidence: "candidate", Basis: "name"}
	exact := edge
	exact.Confidence = "exact"
	exact.Basis = "binding"
	otherKind := edge
	otherKind.Kind = "references"
	otherSite := edge
	otherSite.Span = Span{5, 7}
	run := func(input []Edge) *Index {
		x := NewIndex(nil)
		for _, e := range input {
			x.Add(e)
		}
		return x
	}
	a := run([]Edge{edge, exact, edge, otherKind, otherSite})
	b := run([]Edge{exact, edge, otherKind, otherSite})
	if !reflect.DeepEqual(a.Edges, b.Edges) || len(a.Edges) != 3 || a.EvidenceCount != 4 {
		t.Fatalf("unstable merge: %#v %#v", a, b)
	}
	if a.Edges[0].Confidence != "exact" || len(a.Edges[0].Evidence) != 2 {
		t.Fatal(a.Edges)
	}
	next := edge
	next.Basis = "another_candidate"
	c := run([]Edge{edge, next})
	if c.Edges[0].Confidence != "candidate" {
		t.Fatal(c.Edges)
	}
	conflict := exact
	conflict.Target = DocumentRef("c")
	a.Add(conflict)
	if len(a.Conflicts()) != 1 {
		t.Fatal(a.Conflicts())
	}
}
