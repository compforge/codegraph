package analysis

import (
	"reflect"
	"testing"
)

func TestRelationOccurrenceAndEvidence(t *testing.T) {
	source, target := DocumentRef("a"), DocumentRef("b")
	edge := Edge{Source: source, Target: target, Kind: "calls", Path: "a", Span: Span{1, 3}, Confidence: "scoped", Basis: "name"}
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
	if c.Edges[0].Confidence != "scoped" {
		t.Fatal(c.Edges)
	}
	conflict := exact
	conflict.Target = DocumentRef("c")
	a.Add(conflict)
	if len(a.Conflicts()) != 1 {
		t.Fatal(a.Conflicts())
	}
}

func TestConfidenceDerivedFromAllEvidence(t *testing.T) {
	for _, best := range []Confidence{Heuristic, NameOnly, Scoped, Exact} {
		x := NewIndex(nil)
		e := Edge{Source: DocumentRef("a"), Target: DocumentRef("b"), Kind: "calls", Confidence: Exact,
			Evidence: []Evidence{{Basis: "same_rule", Confidence: Heuristic}, {Basis: "same_rule", Confidence: best}}}
		x.Add(e)
		// An explicit proof list is authoritative even if the edge field disagrees.
		if x.Edges[0].Confidence != best {
			t.Fatal(x.Edges)
		}
		e.Evidence = []Evidence{{Basis: "other_rule", Confidence: Heuristic}}
		x.Add(e)
		if len(x.Edges) != 1 || x.Edges[0].Confidence != best {
			t.Fatal(x.Edges)
		}
		alternative := e
		alternative.Target = DocumentRef("c")
		alternative.Evidence = []Evidence{{Basis: "bounded", Confidence: Scoped}}
		x.Add(alternative)
		if len(x.Conflicts()) != 0 {
			t.Fatal("lower-tier alternative was a conflict")
		}
	}
}

func TestBindingRoutesAndAmbiguity(t *testing.T) {
	a, b := DocumentRef("a"), DocumentRef("b")
	got := UniqueBindingTargets([]BindingTarget{{a, NameOnly}, {a, Exact}})
	if len(got) != 1 || got[0].Confidence != Exact {
		t.Fatal(got)
	}
	got = UniqueBindingTargets([]BindingTarget{{a, Exact}, {b, Heuristic}})
	if len(got) != 2 || got[0].Confidence != Scoped || got[1].Confidence != Heuristic {
		t.Fatal(got)
	}
	for _, c := range []Confidence{Scoped, NameOnly, Heuristic} {
		if TargetState([]BindingTarget{{a, c}}) != Candidates {
			t.Fatal(c)
		}
	}
}
