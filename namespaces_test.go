package codegraph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Exercise query policy on graph values alone, including DAGs and cyclic input.
func namespaceQueryGraph() *Graph {
	g := &Graph{nodes: map[string]Node{}, relations: map[string]Relation{}, timeout: time.Second, limits: graphstore.Limits{Hops: 8, Rows: 100, Bytes: 1 << 20}}
	for _, n := range []Node{
		{ID: "root", Kind: Module}, {ID: "a", Kind: Namespace}, {ID: "b", Kind: Namespace},
		{ID: "class", Kind: Class, Location: &Location{Path: "a.ts"}, Markers: []Marker{{Text: "original"}}},
		{ID: "f", Kind: Method}, {ID: "g", Kind: Function},
	} {
		g.nodes[n.ID] = n
	}
	for _, r := range []Relation{
		{ID: "ra", Source: "root", Target: "a", Kind: Contains, Confidence: Exact},
		{ID: "rb", Source: "root", Target: "b", Kind: Contains, Confidence: Scoped},
		{ID: "ac", Source: "a", Target: "class", Kind: Contains, Confidence: Exact},
		{ID: "bc", Source: "b", Target: "class", Kind: Contains, Confidence: Scoped},
		{ID: "cf", Source: "class", Target: "f", Kind: Contains, Confidence: Exact},
		{ID: "ag", Source: "a", Target: "g", Kind: Contains, Confidence: Exact},
	} {
		g.relations[r.ID] = r
	}
	return g
}

func TestNamespaceQueriesConfidenceIdentityAndDetachment(t *testing.T) {
	g := namespaceQueryGraph()
	ctx := context.Background()
	got, err := g.NamespaceAncestors(ctx, "f", NamespaceOptions{})
	if err != nil || len(got) != 3 || got[0].Node.ID != "class" || got[2].Depth != 3 {
		t.Fatal(got, err)
	}
	got[0].Node.Location.Path = "bad"
	got[0].Node.Markers[0].Text = "bad"
	if g.nodes["class"].Location.Path != "a.ts" || g.nodes["class"].Markers[0].Text != "original" {
		t.Fatal("query aliases graph")
	}
	got, err = g.NamespaceAncestors(ctx, "f", NamespaceOptions{MinConfidence: Scoped})
	if err != nil || len(got) != 4 || got[1].Node.ID != "a" || got[2].Node.ID != "b" {
		t.Fatal(got, err)
	}
	got, err = g.CommonNamespaces(ctx, []string{"f", "g", "f"}, NamespaceOptions{})
	if err != nil || len(got) != 2 || got[0].Node.ID != "a" || got[0].Depth != 2 {
		t.Fatal(got, err)
	}
	self, err := g.NamespaceAncestors(ctx, "class", NamespaceOptions{})
	if err != nil || len(self) != 3 || self[0].Depth != 0 {
		t.Fatal(self, err)
	}
	for _, ids := range [][]string{nil, {"missing"}, {"f", "missing"}} {
		got, err := g.CommonNamespaces(ctx, ids, NamespaceOptions{})
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	for _, opts := range []NamespaceOptions{{MinConfidence: "bogus"}, {Kinds: []NodeKind{Function}}} {
		if _, err := g.NamespaceAncestors(ctx, "f", opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestNamespaceQueriesBudgetsCancellationAndCycles(t *testing.T) {
	for _, kind := range []string{"hops", "rows", "bytes", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			g := namespaceQueryGraph()
			ctx := context.Background()
			want := ErrQueryBudget
			switch kind {
			case "hops":
				g.limits.Hops = 2
			case "rows":
				g.limits.Rows = 2
			case "bytes":
				g.limits.Bytes = 1
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			got, err := g.NamespaceAncestors(ctx, "f", NamespaceOptions{})
			if !errors.Is(err, want) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	g := namespaceQueryGraph()
	g.limits.Hops = 3
	before, err := g.NamespaceAncestors(context.Background(), "f", NamespaceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g.relations["cycle"] = Relation{ID: "cycle", Source: "class", Target: "root", Kind: Contains, Confidence: Exact}
	after, err := g.NamespaceAncestors(context.Background(), "f", NamespaceOptions{})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
}
