package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +spec=CodeGraph identifies Go runtime entrypoints from package, declaration kind and signature, without consumer rules.
func TestGoRuntimeEntrypoints(t *testing.T) {
	docs := []codegraph.Document{
		{Path: "cmd/main.go", Content: []byte(`package main
func main(/* empty */) {}
func init() {}
func init() {}
type worker struct{}
func (worker) main() {}
func (worker) init() {}
func ordinary() {}
`)},
		{Path: "library/library.go", Content: []byte(`package library
func main() {}
func init() {}
`)},
		{Path: "invalid/signatures.go", Content: []byte(`package main
func main(arg int) {}
func init() int { return 1 }
`)},
		{Path: "generic/main.go", Content: []byte(`package main
func main[T any]() {}
`)},
		{Path: "tool.py", Content: []byte("def main(): pass\n")},
	}
	g, _, err := codegraph.Build(context.Background(), "runtime-entries", docs, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	for _, node := range g.Nodes() {
		if node.Kind != codegraph.Function && node.Kind != codegraph.Method {
			continue
		}
		want := node.Kind == codegraph.Function && (node.Location.Path == "cmd/main.go" && (node.Name == "main" || node.Name == "init") || node.Location.Path == "library/library.go" && node.Name == "init")
		if node.Entrypoint != want {
			t.Fatalf("wrong entrypoint classification: %+v want=%v", node, want)
		}
		if want {
			entries++
		}
	}
	rows := query(t, g, `MATCH (n) WHERE n.entrypoint = true RETURN n`, nil)
	if entries != 4 || len(rows) != entries {
		t.Fatalf("entries=%d query=%+v", entries, rows)
	}
	for _, row := range rows {
		node := row["n"].(codegraph.Node)
		if !node.Entrypoint {
			t.Fatalf("query projection lost entrypoint: %+v", node)
		}
		if declares := g.RelationsTo(node.ID, codegraph.Declares); len(declares) != 1 || declares[0].Target != node.ID {
			t.Fatalf("entry lost its declaration: %+v", node)
		}
	}
	for _, language := range []string{"go", "python", "typescript", "rust"} {
		capabilities := codegraph.Capabilities(language)
		if len(capabilities) != 1 || capabilities[0].Entrypoints != (language == "go") {
			t.Fatalf("wrong capability: %+v", capabilities)
		}
	}
}
