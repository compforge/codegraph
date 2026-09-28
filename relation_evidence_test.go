package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestEvidenceDetachedAcrossAccessAndQuery(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "proofs", []Document{{Path: "a.go", Content: []byte("package a; func target(){};func entry(){target();target()}")}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var original Relation
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			original = r
			break
		}
	}
	// Supporting evidence is optional; inject one to exercise pointer ownership.
	loc := original.Location
	original.Evidence[0].Location = &loc
	g.relations[original.ID] = cloneRelation(original)
	g.store, err = g.materialize(ctx, g.nodes, g.relations)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(r Relation) { r.Evidence[0].Basis = "mutated"; r.Evidence[0].Location.Path = "mutated" }
	for _, get := range []func() []Relation{g.Relations, func() []Relation { return g.RelationsFrom(original.Source) }, func() []Relation { return g.RelationsTo(original.Target) }} {
		for _, r := range get() {
			if r.ID == original.ID {
				mutate(r)
			}
		}
	}
	rows, err := g.Query(ctx, "MATCH ()-[r:calls]->() RETURN r", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		r := row["r"].(Relation)
		if r.ID == original.ID {
			mutate(r)
		}
	}
	rows, err = g.Query(ctx, "MATCH p=()-[:calls*1..1]->() RETURN p", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, r := range row["p"].(Path).Relations {
			if r.ID == original.ID {
				mutate(r)
			}
		}
	}
	if !reflect.DeepEqual(g.relations[original.ID], original) {
		t.Fatal("evidence alias escaped")
	}
	rows, err = g.Query(ctx, "MATCH ()-[r:calls]->() RETURN r.evidenceData AS evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var proof []Evidence
		if err := json.Unmarshal([]byte(row["evidence"].(string)), &proof); err != nil || len(proof) == 0 {
			t.Fatal(row, err)
		}
	}
}
func TestEvidenceBudgetIsAtomic(t *testing.T) {
	g, _ := New("budget", Options{MaxEvidence: 1})
	if err := g.AddDocuments(context.Background(), Document{Path: "a.go", Content: []byte("package a;func f(){}")}); err != nil {
		t.Fatal(err)
	}
	_, err := g.Wait(context.Background())
	if !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
		t.Fatal(err, g.Nodes())
	}
}
