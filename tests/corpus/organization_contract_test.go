package corpus_test

import (
	"context"
	cg "github.com/compforge/codegraph"
	"testing"
)

func TestOrganizationAndOccurrenceOracleRejectsLoss(t *testing.T) {
	root := fixtureRoot(t, map[string]string{
		"p.go":     "package corpus; import \"example.org/corpus/lib\"; type Pair struct{A,B int};func Entry(){lib.Target();lib.Target()}",
		"lib/a.go": "package lib;func Target(){}",
		"lib/b.go": "package lib;type Box struct{};func(Box) Run(){}",
	})
	o, docs, _, err := loadOracle(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := observe(context.Background(), o.Module, "organization", docs)
	if err != nil {
		t.Fatal(err)
	}
	e := evaluate(o, a)
	for _, key := range []string{"organizations/Package", "relation_occurrences/declares", "relation_occurrences/contains", "relation_occurrences/imports", "relation_occurrences/calls", "relation_occurrences/references"} {
		m := e.Measurements[key]
		if m == nil || m.Expected == 0 || m.Found != m.Expected || m.Unexpected != 0 {
			t.Fatalf("%s: %+v findings=%v", key, m, e.Findings)
		}
	}
	// Removing one call must not remove its independent reference, nor allow
	// another occurrence between the same two declarations to satisfy it.
	changed := a
	changed.Relations = append([]cg.Relation(nil), a.Relations...)
	for i, r := range changed.Relations {
		if r.Kind == cg.Calls {
			changed.Relations = append(changed.Relations[:i], changed.Relations[i+1:]...)
			break
		}
	}
	bad := evaluate(o, changed)
	if bad.Measurements["relation_occurrences/calls"].Found != 1 || bad.Measurements["relation_occurrences/calls"].Expected != 2 || bad.Measurements["relation_occurrences/references"].Found != e.Measurements["relation_occurrences/references"].Found {
		t.Fatal(bad.Measurements)
	}
	changed = a
	changed.Nodes = append([]cg.Node(nil), a.Nodes...)
	for i, n := range changed.Nodes {
		if n.Kind == cg.Package {
			changed.Nodes[i].QualifiedName = "wrong"
			break
		}
	}
	if m := evaluate(o, changed).Measurements["organizations/Package"]; m.Found != 1 || m.Unexpected != 1 {
		t.Fatal(m)
	}
	// Duplicate IDs must not hide a duplicate declaration-level call occurrence.
	changed = a
	changed.Relations = append([]cg.Relation(nil), a.Relations...)
	for _, r := range a.Relations {
		if r.Kind == cg.Calls {
			changed.Relations = append(changed.Relations, r)
			break
		}
	}
	bad = evaluate(o, changed)
	if bad.Measurements["relation_occurrences/calls"].Unexpected != 1 {
		t.Fatal(bad.Measurements)
	}
}
