package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +case=Captured bindings from one assignment remain sibling declarations regardless of capture order.
// +link=component://internal/language/adapters/python/captures.go
func TestPythonCapturedAssignmentSiblings(t *testing.T) {
	for _, tc := range []struct{ name, assignment, result string }{
		{"tuple", "first, second = 1, 2", "first, second"},
		{"reverse_capture", "first, second = 1, 2", "second, first"},
		{"chain", "first = second = 1", "first, second"},
		{"split", "first = 1\n    second = 2", "first, second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "def outer():\n    " + tc.assignment + "\n    def inner():\n        return " + tc.result + "\n    return inner\n"
			g, _, err := codegraph.Build(context.Background(), tc.name, []codegraph.Document{{Path: "app.py", Content: []byte(source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"first", "second"} {
				nodes := g.Find("app.py", codegraph.Variable, "outer."+name)
				if len(nodes) != 1 {
					t.Fatalf("%s sibling declaration = %+v", name, nodes)
				}
				if rows := query(t, g, `MATCH (:Function {qualifiedName:'outer'})-[:encloses]->(v:Variable {name:$name}) RETURN v`, map[string]any{"name": name}); len(rows) != 1 {
					t.Fatalf("%s lexical parent = %+v", name, rows)
				}
				if rows := query(t, g, `MATCH (:CaptureSite)-[:binds_to]->(v:Variable {name:$name}) RETURN v`, map[string]any{"name": name}); len(rows) == 0 {
					t.Fatalf("%s lost its capture binding", name)
				}
			}
			if rows := query(t, g, `MATCH (:Variable)-[:encloses]->(:Variable) RETURN 1`, nil); len(rows) != 0 {
				t.Fatalf("sibling storage became nested: %+v", rows)
			}
		})
	}
}
