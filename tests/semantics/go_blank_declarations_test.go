package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +spec=Blank Go var/const names declare no symbols while their types and initializers retain uses.
func TestGoBlankValueDeclarations(t *testing.T) {
	source := `package app
 type Contract interface { Run() }
 type Worker struct{}
 func (*Worker) Run() {}
 func sideEffect() int { return 1 }
 var _ Contract = (*Worker)(nil)
 var _, kept = sideEffect(), 2
 const _, limit = 0, 3
 func use() int {
  var _ = sideEffect()
  const _ = 4
  return kept + limit
 }
 `
	g, _, err := codegraph.Build(context.Background(), "blank-values", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]codegraph.Node{}
	for _, node := range g.Nodes() {
		if node.Name == "_" && (node.Kind == codegraph.Variable || node.Kind == codegraph.Constant) {
			t.Fatalf("blank binding became a declaration: %+v", node)
		}
		if node.Kind != codegraph.Reference {
			nodes[node.Name] = node
		}
	}
	for _, name := range []string{"Contract", "Worker", "kept", "limit"} {
		target, ok := nodes[name]
		if !ok || len(g.RelationsTo(target.ID, codegraph.References)) == 0 {
			t.Fatalf("lost declaration or reference to %s", name)
		}
	}
	calls := g.RelationsTo(nodes["sideEffect"].ID, codegraph.Calls)
	if len(calls) != 2 {
		t.Fatalf("lost discarded initializer calls: %+v", calls)
	}
}
