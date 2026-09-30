package codegraph_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/compforge/codegraph"
	"github.com/odvcencio/gotreesitter/grammars"
)

// An extension is registered through the upstream grammar mechanism, not a
// hardcoded CodeGraph allowlist or an AST-bearing public graph API.
func TestRegisteredExtension(t *testing.T) {
	entry := *grammars.DetectLanguageByName("python")
	entry.Name = "codegraph-test-extension"
	entry.Extensions = []string{".cgtest"}
	entry.TagsQuery = "(function_definition name: (identifier) @name) @definition.function"
	grammars.Register(entry)
	if codegraph.Language("app.cgtest") != entry.Name || !slices.Contains(codegraph.Languages(), entry.Name) {
		t.Fatal("extension not discoverable")
	}
	cap := codegraph.Capabilities(entry.Name)
	if len(cap) != 1 || !slices.Contains(cap[0].Declarations, codegraph.Function) || slices.Contains(cap[0].Relations, codegraph.Calls) {
		t.Fatal(cap)
	}
	g, r, err := codegraph.Build(context.Background(), "rev", []codegraph.Document{{Path: "app.cgtest", Content: []byte("def work():\n    pass\n")}}, codegraph.Options{})
	if err != nil || (len(r.Diagnostics) == 0) {
		t.Fatal(r, err)
	}
	rows, err := g.Query(context.Background(), `MATCH (n:Function {name:'work'}) RETURN n`, nil)
	if err != nil || len(rows) != 1 || rows[0]["n"].(codegraph.Node).Language != entry.Name {
		t.Fatal(rows, err)
	}

	entry.Name = "codegraph-test-unavailable"
	entry.Extensions = []string{".cgunavailable"}
	entry.Language = nil
	grammars.Register(entry)
	_, r, err = codegraph.Build(context.Background(), "rev", []codegraph.Document{{Path: "app.cgunavailable", Content: []byte("code")}}, codegraph.Options{})
	if err != nil || (len(r.Diagnostics) == 0) || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "parse_error" {
		t.Fatal(r, err)
	}
}

// Public values keep their JSON field names when implementation packages move.
func TestSourceLocationJSONContract(t *testing.T) {
	loc := codegraph.Location{Path: "a.go", StartByte: 3, EndByte: 7, Line: 1, Column: 4, EndLine: 1, EndColumn: 8}
	for _, value := range []any{
		codegraph.Node{Location: &loc}, codegraph.Relation{Location: loc},
		codegraph.Evidence{Location: &loc}, codegraph.Marker{Location: loc},
		codegraph.Diagnostic{Location: loc},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		var got codegraph.Location
		if err := json.Unmarshal(fields["location"], &got); err != nil || got != loc {
			t.Fatalf("%T source location changed: %s (%v)", value, data, err)
		}
		if _, leaked := fields["SourceLocation"]; leaked {
			t.Fatalf("implementation name escaped: %s", data)
		}
	}
}
