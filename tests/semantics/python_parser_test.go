package semantics_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

// +link=https://github.com/odvcencio/gotreesitter/issues/1274
// +case=`Python splat suffixes remain inside the unpacked expression, while calls and references retain their owner`
func TestPythonSplatSuffixFactsAndGraph(t *testing.T) {
	object := codegraph.Expression{Kind: "identifier", Text: "items"}
	index := codegraph.Expression{Kind: "integer", Text: "0"}
	attribute := codegraph.Expression{Kind: "attribute", Text: "values", Children: []codegraph.Expression{object}}
	subscript := codegraph.Expression{Kind: "subscript", Children: []codegraph.Expression{object, index}}
	for _, tc := range []struct {
		name, expression string
		want             codegraph.Expression
	}{
		{"attribute", "items.values", attribute},
		{"subscript", "items[0]", subscript},
		{"attribute_subscript", "items.values[0]", codegraph.Expression{Kind: "subscript", Children: []codegraph.Expression{attribute, index}}},
		{"subscript_attribute", "items[0].values", codegraph.Expression{Kind: "attribute", Text: "values", Children: []codegraph.Expression{subscript}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "def sink(*args):\n    pass\ndef target():\n    pass\ndef entry(items):\n    sink(*" + tc.expression + ", target)\n"
			g, err := codegraph.NewBuilder("splat", codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			doc := codegraph.Document{Path: "app.py", Content: []byte(source)}
			facts, err := g.Extract(context.Background(), doc)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts.Statements) != 3 || len(facts.Statements[2].Body) != 1 {
				t.Fatalf("statements = %+v", facts.Statements)
			}
			call := facts.Statements[2].Body[0].Value
			want := codegraph.Expression{Kind: "call", Children: []codegraph.Expression{
				{Kind: "identifier", Text: "sink"},
				{Kind: "list_splat", Children: []codegraph.Expression{tc.want}},
				{Kind: "identifier", Text: "target"},
			}}
			if !reflect.DeepEqual(call, want) {
				t.Fatalf("call expression = %#v, want %#v", call, want)
			}
			if err := g.AddDocuments(context.Background(), doc); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertPythonParserGraph(t, g.Result(), source, "sink", "sink(*"+tc.expression+", target)")
		})
	}
}

// +link=https://github.com/odvcencio/gotreesitter/issues/1275
// +case=`Escaped multiline strings preserve surrounding declarations and edges without treating string contents as references`
func TestPythonEscapedStringGraph(t *testing.T) {
	for _, tc := range []struct{ name, literal string }{
		{"triple_two_slashes", "\"\"\"target()" + `\\` + "\n\"\"\""},
		{"triple_four_slashes", "\"\"\"target()" + `\\\\` + "\n\"\"\""},
		{"single_continuation", "\"target()" + `\` + "\ntext\""},
		{"raw_triple", "r\"\"\"target()" + `\\` + "\n\"\"\""},
		{"bytes_triple", "b\"\"\"target()" + `\\` + "\n\"\"\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "def sink():\n    pass\ndef target():\n    pass\ndef entry():\n    text = " + tc.literal + "\n    sink()\n    return target\n"
			g, report, err := codegraph.Build(context.Background(), "escapes", []codegraph.Document{{Path: "app.py", Content: []byte(source)}}, codegraph.Options{})
			if err != nil || len(report.Diagnostics) != 0 {
				t.Fatalf("build: %v, diagnostics: %+v", err, report.Diagnostics)
			}
			assertPythonParserGraph(t, g, source, "sink", "sink()")
		})
	}
}

// Check consumer-visible nodes, edge ownership, confidence and source spans.
// In particular, mentioning target in a string must not create an extra edge.
func assertPythonParserGraph(t *testing.T, g *codegraph.Graph, source, callee, callText string) {
	t.Helper()
	for _, name := range []string{"entry", "target", callee} {
		nodes := g.Find("app.py", codegraph.Function, name)
		if len(nodes) != 1 {
			t.Fatalf("%s nodes = %+v", name, nodes)
		}
		n := nodes[0]
		if !strings.HasPrefix(source[n.Location.StartByte:n.Location.EndByte], "def "+name+"(") {
			t.Fatalf("wrong declaration span: %+v", n)
		}
	}
	entry := g.Find("app.py", codegraph.Function, "entry")[0]
	sink := g.Find("app.py", codegraph.Function, callee)[0]
	target := g.Find("app.py", codegraph.Function, "target")[0]
	calls := g.RelationsFrom(entry.ID, codegraph.Calls)
	if len(calls) != 1 || calls[0].Target != sink.ID || calls[0].Confidence != codegraph.Exact {
		t.Fatalf("entry calls = %+v", calls)
	}
	if got := source[calls[0].Location.StartByte:calls[0].Location.EndByte]; got != callText {
		t.Fatalf("call span = %q, want %q", got, callText)
	}
	refs := g.RelationsFrom(entry.ID, codegraph.References)
	if len(refs) != 2 {
		t.Fatalf("entry references = %+v", refs)
	}
	counts := map[string]int{}
	for _, ref := range refs {
		counts[ref.Target]++
		if ref.Confidence != codegraph.Exact {
			t.Fatalf("reference confidence = %+v", ref)
		}
		name := callee
		if ref.Target == target.ID {
			name = "target"
		}
		if got := source[ref.Location.StartByte:ref.Location.EndByte]; got != name {
			t.Fatalf("reference span = %q, want %q", got, name)
		}
	}
	if counts[sink.ID] != 1 || counts[target.ID] != 1 {
		t.Fatalf("reference targets = %v", counts)
	}
}
