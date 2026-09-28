package codegraph

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// All tiers must survive publication, properties, relation values and paths.
func TestConfidencePublicationAndQuery(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "tiers", []Document{
		{Path: "a.go", Content: []byte("package a;type I interface { Run() };type Box struct{};func(Box) Run(){};func work(){};func entry(b Box){work();b.Run()}")},
		{Path: "a.ts", Content: []byte("class Box { run() {} };function entry(x: any) { x.run(); }")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := map[Confidence]bool{}
	originals := map[string]Relation{}
	for _, r := range g.Relations() {
		var derived Confidence
		for _, e := range r.Evidence {
			if !e.Confidence.Valid() {
				t.Fatal(e)
			}
			derived = derived.Stronger(e.Confidence)
		}
		if !r.Confidence.Valid() || r.Confidence != derived {
			t.Fatal(r)
		}
		found[r.Confidence] = true
		originals[r.ID] = r
	}
	if len(found) != 4 {
		t.Fatal(found)
	}
	rows, err := g.Query(ctx, "MATCH ()-[r]->() RETURN r, r.confidence AS confidence, r.evidenceData AS evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		r := row["r"].(Relation)
		var proof []Evidence
		if err := json.Unmarshal([]byte(row["evidence"].(string)), &proof); err != nil {
			t.Fatal(err)
		}
		if row["confidence"] != string(r.Confidence) || !reflect.DeepEqual(proof, r.Evidence) || !reflect.DeepEqual(r, originals[r.ID]) {
			t.Fatal(row)
		}
	}
	rows, err = g.Query(ctx, "MATCH ()-[r]->() WHERE r.confidence IN ['exact', 'scoped'] RETURN r", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, r := range originals {
		if r.Confidence.AtLeast(Scoped) {
			want++
		}
	}
	if len(rows) != want {
		t.Fatalf("threshold query: got %d want %d", len(rows), want)
	}
	for _, row := range rows {
		if !row["r"].(Relation).Confidence.AtLeast(Scoped) {
			t.Fatal(row)
		}
	}
	rows, err = g.Query(ctx, "MATCH p=()-[:calls*1..1]->() RETURN p", nil)
	if err != nil || len(rows) == 0 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		for _, r := range row["p"].(Path).Relations {
			if !reflect.DeepEqual(r, originals[r.ID]) {
				t.Fatal(r)
			}
		}
	}
}
