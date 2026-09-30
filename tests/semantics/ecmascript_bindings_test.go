package semantics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

// +case=`Loop declarations and catch parameters have their own source spans; assignment targets and destructuring are not simple declarations`
func TestECMAScriptControlBindingDeclarations(t *testing.T) {
	source := `async function run() {
  for (const item of items) { consume(item); }
  for (let key in object) { consume(key); }
  for (var value of values) { consume(value); }
  for await (const row of rows) { consume(row); }
  for (let index = 0; index < 2; index++) { consume(index); }
  try {} catch (error) { consume(error); }
  try {} catch (error) { consume(error); }
  for (existing of items) { consume(existing); }
  for (const {part} of items) { consume(part); }
  try {} catch ({message}) { consume(message); }
  try {} catch {}
}`
	for _, path := range []string{"app.js", "app.ts", "app.tsx"} {
		t.Run(path, func(t *testing.T) {
			g, report, err := codegraph.Build(context.Background(), "bindings", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
			if err != nil || hasDiagnostic(report, "outline_incomplete") {
				t.Fatal(report, err)
			}
			facts, err := g.Extract(context.Background(), codegraph.Document{Path: path, Content: []byte(source)})
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			for _, node := range g.Nodes() {
				if node.Kind != codegraph.Variable {
					continue
				}
				counts[node.Name]++
				want := node.Name
				if node.Name == "index" {
					want = "index = 0"
				}
				if node.Location == nil || source[node.Location.StartByte:node.Location.EndByte] != want {
					t.Fatalf("wrong declaration span: %+v", node)
				}
				parents := g.RelationsTo(node.ID, codegraph.Contains)
				if len(parents) != 1 {
					t.Fatalf("missing owner: %+v", node)
				}
				owner, ok := g.Node(parents[0].Source)
				if !ok || owner.Name != "run" {
					t.Fatalf("wrong owner: %+v", owner)
				}
				for _, ref := range facts.References {
					if ref.Location.StartByte == node.Location.StartByte {
						t.Fatalf("declaration name became a reference fact: %+v", ref)
					}
				}
			}
			for _, name := range []string{"item", "key", "value", "row", "index", "error"} {
				want := 1
				if name == "error" {
					want = 2
				}
				if counts[name] != want {
					t.Fatalf("%s: got %d declarations, want %d; all=%v", name, counts[name], want, counts)
				}
				delete(counts, name)
			}
			if len(counts) != 0 {
				t.Fatalf("invented declarations: %v", counts)
			}
		})
	}
}

func TestTypeScriptCatchAnnotationSpan(t *testing.T) {
	const source = "function run() { try {} catch (error: unknown) { consume(error); } }"
	for _, path := range []string{"app.ts", "app.tsx"} {
		g, report, err := codegraph.Build(context.Background(), "typed-catch", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
		if err != nil || hasDiagnostic(report, "outline_incomplete") {
			t.Fatal(report, err)
		}
		found := false
		for _, node := range g.Nodes() {
			if node.Kind == codegraph.Variable && node.Name == "error" {
				found = true
				if node.Location == nil || source[node.Location.StartByte:node.Location.EndByte] != "error: unknown" {
					t.Fatalf("typed catch range: %+v", node)
				}
			}
		}
		if !found {
			t.Fatal("missing typed catch declaration")
		}
	}
}

// +case=`A loop or catch binding cannot become an exact function target or leak to an unrelated use`
func TestECMAScriptControlBindingShadowing(t *testing.T) {
	const source = `function work() {}
function run(items) {
  for (const work of items) { work(); }
  try {} catch (work) { work(); }
}
for (const scoped of items) { consume(scoped); }
consume(scoped);
`
	for _, path := range []string{"app.js", "app.ts", "app.tsx"} {
		g, _, err := codegraph.Build(context.Background(), "shadow", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		outside := strings.LastIndex(source, "scoped")
		for _, edge := range g.Relations() {
			if edge.Kind == codegraph.Calls && edge.Confidence == codegraph.Exact {
				t.Fatalf("shadowed call became exact: %+v", edge)
			}
			if edge.Kind == codegraph.References && edge.Confidence == codegraph.Exact && edge.Location.StartByte == outside {
				t.Fatalf("loop binding leaked outside scope: %+v", edge)
			}
		}
	}
}
