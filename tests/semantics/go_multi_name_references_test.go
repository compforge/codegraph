package semantics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

// +spec=Each use of a multi-name Go var or const binds to its own declaration, including under shadowing.
func TestGoMultiNameReferences(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		kind         codegraph.NodeKind
		uses         int
	}{
		{"local-var", `package app
func use() (string, string) {
 var first, second string
 scan(&first, &second)
 return first, second
}
func scan(a, b *string) {}`, codegraph.Variable, 2},
		{"package-var", `package app
var first, second string
func use() (string, string) {
 scan(&first, &second)
 return first, second
}
func scan(a, b *string) {}`, codegraph.Variable, 2},
		{"local-const", `package app
func use() int {
 const first, second = 1, 2
 return first + second
}`, codegraph.Constant, 1},
		{"package-const", `package app
const first, second = 1, 2
func use() int { return first + second }`, codegraph.Constant, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := codegraph.Build(context.Background(), "multi-name", []codegraph.Document{{Path: "app.go", Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"first", "second"} {
				nodes := []codegraph.Node{}
				for _, node := range g.Nodes() {
					if node.Kind == tc.kind && node.Name == name {
						nodes = append(nodes, node)
					}
				}
				if len(nodes) != 1 {
					t.Fatalf("%s: declarations=%v", name, nodes)
				}
				// Check both the declaration-level edges and source-use edges consumed by graph clients.
				rows := query(t, g, `MATCH (source)-[r:references]->(target) WHERE target.id = $id RETURN source,r`, map[string]any{"id": nodes[0].ID})
				counts := map[codegraph.NodeKind]int{}
				for _, row := range rows {
					source, edge := row["source"].(codegraph.Node), row["r"].(codegraph.Relation)
					counts[source.Kind]++
					if edge.Confidence != codegraph.Exact || tc.source[edge.Location.StartByte:edge.Location.EndByte] != name {
						t.Fatalf("wrong reference: %+v", row)
					}
				}
				if counts[codegraph.Function] != tc.uses || counts[codegraph.Reference] != tc.uses || len(counts) != 2 {
					t.Fatalf("%s: incoming references=%v, want %d in each view", name, counts, tc.uses)
				}
			}
		})
	}
}

func TestGoMultiNameReferenceShadowing(t *testing.T) {
	source := `package app
var first, second string
func use() {
 var first, second string
 _ = &first
 _ = &second
 {
  const first, second = 1, 2
  _ = first + second
 }
 _ = first + second
}
func parameter(first string) { _ = first }
`
	g, _, err := codegraph.Build(context.Background(), "shadowing", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range g.Nodes() {
		if node.Kind != codegraph.Reference || (node.Name != "first" && node.Name != "second") {
			continue
		}
		edges := g.RelationsFrom(node.ID, codegraph.References)
		if node.Location.StartByte > strings.Index(source, "func parameter") {
			if len(edges) != 0 {
				t.Fatalf("parameter leaked to outer declaration: %+v", edges)
			}
			continue
		}
		if len(edges) != 1 {
			t.Fatalf("missing/ambiguous target for %+v: %+v", node, edges)
		}
		declaration := "var first, second string"
		start := strings.Index(source, "func use")
		if node.Location.StartByte > strings.Index(source, "const first") && node.Location.StartByte < strings.Index(source, "\n }") {
			declaration = "const first, second = 1, 2"
		}
		want := start + strings.Index(source[start:], declaration) + strings.Index(declaration, node.Name)
		found := false
		for _, target := range g.Nodes() {
			if target.ID == edges[0].Target {
				found = target.NameLocation != nil && target.NameLocation.StartByte == want && edges[0].Confidence == codegraph.Exact
			}
		}
		if !found {
			t.Fatalf("wrong shadowed target for %+v: %+v; want declaration at %d", node, edges, want)
		}
	}
}
