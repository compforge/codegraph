package codegraph_test

import (
	"context"
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
	if err != nil || r.Complete {
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
	if err != nil || r.Complete || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "parse_error" {
		t.Fatal(r, err)
	}
}
