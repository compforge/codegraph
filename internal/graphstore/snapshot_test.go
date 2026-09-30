package graphstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/compforge/codegraph/internal/graphmodel"
)

func TestEvidenceDetachedAcrossAccessAndQuery(t *testing.T) {
	ctx := context.Background()
	loc := graphmodel.Location{Path: "a.go", Line: 1, Column: 1}
	nodes := map[string]graphmodel.Node{"a": {ID: "a", Kind: graphmodel.Function, Name: "entry", Location: &loc}, "b": {ID: "b", Kind: graphmodel.Function, Name: "target", Location: &loc}}
	relations := map[string]graphmodel.Relation{"r": {ID: "r", Source: "a", Target: "b", Kind: graphmodel.Calls, Confidence: graphmodel.Exact, Evidence: []graphmodel.Evidence{{Basis: "call", Confidence: graphmodel.Exact}}, Location: loc}}
	g := NewSnapshot("proofs", nodes, relations, graphmodel.BuildReport{Snapshot: "proofs"}, Limits{Rows: 100, Bytes: 1 << 20, Hops: 8}, time.Second)
	var err error
	var original graphmodel.Relation
	for _, r := range g.Relations() {
		if r.Kind == graphmodel.Calls {
			original = r
			break
		}
	}
	// Supporting evidence is optional; inject one to exercise pointer ownership.
	proofLocation := original.Location
	original.Evidence[0].Location = &proofLocation
	g.relations[original.ID] = graphmodel.CloneRelation(original)
	g.store, err = g.materialize(ctx, g.nodes, g.relations)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(r graphmodel.Relation) { r.Evidence[0].Basis = "mutated"; r.Evidence[0].Location.Path = "mutated" }
	for _, get := range []func() []graphmodel.Relation{g.Relations, func() []graphmodel.Relation { return g.RelationsFrom(original.Source) }, func() []graphmodel.Relation { return g.RelationsTo(original.Target) }} {
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
		r := row["r"].(graphmodel.Relation)
		if r.ID == original.ID {
			mutate(r)
		}
	}
	rows, err = g.Query(ctx, "MATCH p=()-[:calls*1..1]->() RETURN p", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, r := range row["p"].(graphmodel.Path).Relations {
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
		var proof []graphmodel.Evidence
		if err := json.Unmarshal([]byte(row["evidence"].(string)), &proof); err != nil || len(proof) == 0 {
			t.Fatal(row, err)
		}
	}
}

// Typed navigation never needs the query index. A canceled first query must
// leave index construction retryable for later, concurrent readers.
func TestSnapshotLazyQueryIndex(t *testing.T) {
	g := NewSnapshot("lazy", map[string]graphmodel.Node{"a": {ID: "a", Kind: graphmodel.Function, Name: "A", QualifiedName: "A", Location: &graphmodel.Location{Path: "a.go"}}}, nil, graphmodel.BuildReport{Snapshot: "lazy"}, Limits{Rows: 10, Bytes: 1 << 20, Hops: 8}, time.Second)
	if len(g.Nodes()) != 1 || len(g.Find("a.go", graphmodel.Function, "A")) != 1 || g.store != nil {
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
