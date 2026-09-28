package corpus_test

import (
	cg "github.com/compforge/codegraph"
	"testing"
)

func TestTierScoringAndIndependentTargetSet(t *testing.T) {
	for _, tier := range []cg.Confidence{cg.Exact, cg.Scoped, cg.NameOnly, cg.Heuristic, legacyCandidate} {
		e := evaluation{Bindings: map[cg.RelationKind]*bindings{}, Unassessed: map[string]int{}}
		loc := cg.Location{Path: "a", StartByte: 10, EndByte: 12}
		truth := map[string]occurrence{location(loc).key(): {Site: location(loc), Target: "good", Class: "internal"}}
		relations := []cg.Relation{
			{Kind: cg.Calls, Target: "good", Location: loc, Confidence: tier},
			{Kind: cg.Calls, Target: "other", Location: loc, Confidence: tier},
			{Kind: cg.Calls, Target: "unknown", Location: cg.Location{Path: "b"}, Confidence: tier},
		}
		compareBindings(&e, cg.Calls, truth, map[string]string{"good": "good"}, observed{Relations: relations})
		b := e.Bindings[cg.Calls]
		v := b.tier(tier)
		if v.Hit != 1 || v.Other != 1 || v.Unassessed != 1 || b.MaxTargets != 2 || b.Hit != 1 || b.Expected != 1 {
			t.Fatal(tier, b, v)
		}
		if tier != cg.Exact && b.tier(cg.Exact).Other != 0 {
			t.Fatal("non-exact mismatch became exact error")
		}
	}
}

func TestReclassificationDoesNotCreateBaselineRegression(t *testing.T) {
	prior := runReport{SchemaVersion: 3, Status: "measured", Evaluation: &evaluation{Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 1, Hit: 1, MaxTargets: 2, ByConfidence: map[cg.Confidence]*tierBindings{legacyCandidate: {Hit: 1, Other: 1}}}}}}
	now := prior
	now.Evaluation = &evaluation{Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 1, Hit: 1, MaxTargets: 2, ByConfidence: map[cg.Confidence]*tierBindings{cg.Scoped: {Hit: 1}, cg.NameOnly: {Other: 1}}}}}
	if regressions, err := compareBaseline(prior, now); err != nil || len(regressions) != 0 {
		t.Fatal(regressions, err)
	}
	now.Evaluation.Bindings[cg.Calls].MaxTargets++
	if regressions, err := compareBaseline(prior, now); err != nil || len(regressions) == 0 {
		t.Fatal("missed topology expansion", regressions, err)
	}
}
