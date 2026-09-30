package graphstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/compforge/codegraph/internal/model"
)

func TestEvidenceDetachedAcrossAccessAndQuery(t *testing.T) {
	ctx := context.Background()
	loc := model.Location{Path: "a.go", Line: 1, Column: 1}
	nodes := map[string]model.Node{"a": {ID: "a", Kind: model.Function, Name: "entry", Location: &loc}, "b": {ID: "b", Kind: model.Function, Name: "target", Location: &loc}}
	relations := map[string]model.Relation{"r": {ID: "r", Source: "a", Target: "b", Kind: model.Calls, Confidence: model.Exact, Evidence: []model.Evidence{{Basis: "call", Confidence: model.Exact}}, Location: loc}}
	g := NewSnapshot("proofs", nodes, relations, model.BuildReport{Snapshot: "proofs"}, Limits{Rows: 100, Bytes: 1 << 20, Hops: 8}, time.Second)
	var err error
	var original model.Relation
	for _, r := range g.Relations() {
		if r.Kind == model.Calls {
			original = r
			break
		}
	}
	// Supporting evidence is optional; inject one to exercise pointer ownership.
	proofLocation := original.Location
	original.Evidence[0].Location = &proofLocation
	g.relations[original.ID] = model.CloneRelation(original)
	g.store, err = g.materialize(ctx, g.nodes, g.relations)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(r model.Relation) { r.Evidence[0].Basis = "mutated"; r.Evidence[0].Location.Path = "mutated" }
	for _, get := range []func() []model.Relation{g.Relations, func() []model.Relation { return g.RelationsFrom(original.Source) }, func() []model.Relation { return g.RelationsTo(original.Target) }} {
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
		r := row["r"].(model.Relation)
		if r.ID == original.ID {
			mutate(r)
		}
	}
	rows, err = g.Query(ctx, "MATCH p=()-[:calls*1..1]->() RETURN p", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, r := range row["p"].(model.Path).Relations {
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
		var proof []model.Evidence
		if err := json.Unmarshal([]byte(row["evidence"].(string)), &proof); err != nil || len(proof) == 0 {
			t.Fatal(row, err)
		}
	}
}

// Typed navigation never needs the query index. A canceled first query must
// leave index construction retryable for later, concurrent readers.
func TestSnapshotLazyQueryIndex(t *testing.T) {
	g := NewSnapshot("lazy", map[string]model.Node{"a": {ID: "a", Kind: model.Function, Name: "A", QualifiedName: "A", Location: &model.Location{Path: "a.go"}}}, nil, model.BuildReport{Snapshot: "lazy"}, Limits{Rows: 10, Bytes: 1 << 20, Hops: 8}, time.Second)
	if len(g.Nodes()) != 1 || len(g.Find("a.go", model.Function, "A")) != 1 || g.store != nil {
		t.Fatal("typed navigation allocated query storage")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Query(ctx, "MATCH (n) RETURN n", nil); !errors.Is(err, context.Canceled) || g.store != nil {
		t.Fatal("canceled query published an index", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			rows, err := g.Query(context.Background(), "MATCH (n:Function) RETURN n", nil)
			if err != nil || len(rows) != 1 {
				t.Errorf("concurrent first query: %v %v", rows, err)
			}
		})
	}
	wg.Wait()
	if g.store == nil {
		t.Fatal("successful query did not retain its index")
	}
}
