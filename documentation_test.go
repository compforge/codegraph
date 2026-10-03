package codegraph

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDeclarationDocumentation(t *testing.T) {
	tests := []struct {
		name, path, source string
		want               map[string]string
	}{
		{"go", "api.go", `package api
// Run processes 输入.
//
// +doc=explicit contract
func Run() {}
type A struct {
    // Value is owned by A.
    Value string
}
type B struct {}
// Save A.
func (a A) Save() {}
/* Save B.
   More details. */
func (b B) Save() {}
// This describes the group, not either individual type.
type (
    First int
    // Second alone.
    Second int
)
// Single type.
type Single int
type Store interface {
    // Load retrieves a value.
    Load() string
}
var Unrelated = 1 // trailing comment
func Bare() {}
`, map[string]string{
			"Run":     "// Run processes 输入.\n//\n// +doc=explicit contract",
			"A.Value": "// Value is owned by A.",
			"A.Save":  "// Save A.", "B.Save": "/* Save B.\n   More details. */",
			"Second": "// Second alone.", "Single": "// Single type.",
			"Store.Load": "// Load retrieves a value.",
		}},
		{"go-crlf", "crlf.go", "package p\r\n/* 文档\r\n second */\r\nfunc Run() {}\r\n", map[string]string{"Run": "/* 文档\r\n second */"}},
		{"python", "api.py", `"""Module documentation is not a declaration docstring."""
@decorate
class A:
    r"""Class docs with literal \n.

    Further details.
    """
    # +doc=explicit method contract
    @decorate
    async def run(self):
        # A comment before the first statement is allowed.
        """A.run docs."""
        return 1

class B:
    def run(self):
        "B.run docs."
        return 2

def outer():
    "Outer docs."
    def inner():
        'Inner docs.'
        return 3
    return inner()

def later():
    pass
    "Not a docstring."

def formatted():
    f"Not a docstring {1}."

def binary():
    b"Not a docstring."

def tuple_value():
    ("Not", "a docstring")

def concatenated_bytes():
    b"Not " b"a docstring"

def joined():
    ("First "
     "second.")

def inline(): "Inline docs."
`, map[string]string{
			"A":     "r\"\"\"Class docs with literal \\n.\n\n    Further details.\n    \"\"\"",
			"A.run": "\"\"\"A.run docs.\"\"\"", "B.run": "\"B.run docs.\"",
			"outer": "\"Outer docs.\"", "outer.inner": "'Inner docs.'",
			"joined": "(\"First \"\n     \"second.\")", "inline": "\"Inline docs.\"",
		}},
		{"typescript", "api.ts", `/** Exported docs. */
export function run() {}
/** Arrow docs. */
export const arrow = () => 1;
/** Class docs. */
export class A {
    /** Method docs. */
    run() {}
}
/** Alias docs. */
export type Alias = string;
/** Ambient docs. */
export declare const external: number;
/** Detached docs. */

function bare() {}
/** Ambiguous shared docs. */
const first = 1, second = 2;
const previous = 3; /** Trailing docs. */
function after() {}
// Ordinary implementation comment.
function unmarked() {}
`, map[string]string{
			"run": "/** Exported docs. */", "arrow": "/** Arrow docs. */",
			"A": "/** Class docs. */", "A.run": "/** Method docs. */",
			"Alias": "/** Alias docs. */", "external": "/** Ambient docs. */",
		}},
		{"javascript", "api.js", "/** JS docs. */\nexport default function run() {}", map[string]string{"run": "/** JS docs. */"}},
		{"tsx", "view.tsx", "/** View docs. */\nexport function View() { return <div/>; }", map[string]string{"View": "/** View docs. */"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			facts := extractTestFacts(t, newTestExtractor(t, nil), tt.path, tt.source)
			seen := map[string]string{}
			for _, d := range facts.Declarations {
				want := tt.want[d.QualifiedName]
				if want == "" {
					if len(d.Documentation) != 0 {
						t.Errorf("unexpected documentation on %s: %+v", d.QualifiedName, d.Documentation)
					}
					continue
				}
				if len(d.Documentation) != 1 {
					t.Errorf("%s docs = %+v; want %q", d.QualifiedName, d.Documentation, want)
					continue
				}
				doc := d.Documentation[0]
				if doc.Text != want {
					t.Errorf("%s text = %q; want %q", d.QualifiedName, doc.Text, want)
				}
				assertDocumentationLocation(t, tt.path, tt.source, doc)
				seen[d.QualifiedName] = doc.Text
			}
			if !reflect.DeepEqual(seen, tt.want) {
				t.Errorf("documentation = %#v; want %#v", seen, tt.want)
			}
			builder, err := NewBuilder("revision", Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := builder.Add(facts); err != nil {
				t.Fatal(err)
			}
			graph, _, err := builder.Build(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range facts.Declarations {
				nodes := graph.Find(tt.path, d.Kind, d.QualifiedName)
				if len(nodes) != 1 || !reflect.DeepEqual(nodes[0].Documentation, d.Documentation) {
					t.Errorf("graph documentation differs for %s: %+v", d.QualifiedName, nodes)
				}
			}
		})
	}
}

func assertDocumentationLocation(t *testing.T, path, source string, doc Documentation) {
	t.Helper()
	loc := doc.Location
	if loc.Path != path || loc.StartByte < 0 || loc.EndByte > len(source) || loc.StartByte >= loc.EndByte {
		t.Fatalf("invalid documentation location: %+v", doc)
	}
	if source[loc.StartByte:loc.EndByte] != doc.Text {
		t.Fatal("documentation lost original source bytes", doc)
	}
	if loc.Line != strings.Count(source[:loc.StartByte], "\n")+1 || loc.EndLine != strings.Count(source[:loc.EndByte], "\n")+1 {
		t.Fatal("documentation line coordinates differ from bytes", doc)
	}
	if loc.Column != loc.StartByte-strings.LastIndexByte(source[:loc.StartByte], '\n') || loc.EndColumn != loc.EndByte-strings.LastIndexByte(source[:loc.EndByte], '\n') {
		t.Fatal("documentation columns differ from bytes", doc)
	}
}

func TestDocumentationOwnershipAndQuery(t *testing.T) {
	ctx := context.Background()
	cache, err := NewExtractionCache(4, 4096)
	if err != nil {
		t.Fatal(err)
	}
	extractor := newTestExtractor(t, cache)
	source := "package p\n// Run documentation.\n// +doc=authored marker\nfunc Run() {}\n"
	facts := extractTestFacts(t, extractor, "a.go", source)
	original := facts.Declarations[0].Documentation[0]
	facts.Declarations[0].Documentation[0].Text = "corrupted"
	builder, err := NewBuilder("first", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(facts); err != nil {
		t.Fatal(err)
	}
	graph, _, err := builder.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.Find("a.go", Function, "Run")[0]
	if !reflect.DeepEqual(node.Documentation, []Documentation{original}) {
		t.Fatal("Facts mutation altered build input", node)
	}
	if len(node.Markers) != 1 || node.Markers[0].Kind != Doc || node.Markers[0].Text != "authored marker" {
		t.Fatal("ordinary docs changed explicit markers", node.Markers)
	}
	node.Documentation[0].Location.Path = "corrupted"
	node.Documentation[0].Text = "corrupted"
	fetched, ok := graph.Node(node.ID)
	if !ok || fetched.Documentation[0] != original {
		t.Fatal("Find aliases graph", fetched)
	}
	fetched.Documentation[0].Text = "corrupted"
	rows := query(t, graph, `MATCH (n:Function {name:'Run'}) RETURN n, n.documentation AS docs, n.documentationData AS data, n.doc AS markers`, nil)
	got := rows[0]["n"].(Node)
	var docs []Documentation
	if err := json.Unmarshal([]byte(rows[0]["data"].(string)), &docs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(docs, []Documentation{original}) || !reflect.DeepEqual(got.Documentation, docs) {
		t.Fatal("query dropped documentation", rows)
	}
	encoded, err := json.Marshal(rows[0]["docs"])
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	if err := json.Unmarshal(encoded, &texts); err != nil || !reflect.DeepEqual(texts, []string{original.Text}) {
		t.Fatal("query text projection differs", rows)
	}
	got.Documentation[0].Text = "corrupted"
	if graph.Find("a.go", Function, "Run")[0].Documentation[0] != original {
		t.Fatal("Query aliases graph")
	}
	again := extractTestFacts(t, extractor, "a.go", source)
	if again.Declarations[0].Documentation[0] != original {
		t.Fatal("Facts mutation altered cache")
	}
	updated := extractTestFacts(t, extractor, "a.go", strings.ReplaceAll(source, "Run documentation", "Changed documentation"))
	if updated.Declarations[0].Documentation[0].Text == original.Text {
		t.Fatal("document versions shared stale documentation")
	}
	if graph.Find("a.go", Function, "Run")[0].Documentation[0] != original {
		t.Fatal("new extraction altered prior graph")
	}
	view, err := facts.View()
	if err != nil || view.Declarations[0].Documentation[0] != original {
		t.Fatal("Facts view lost immutable documentation", err)
	}
}

func TestDocumentationCapabilities(t *testing.T) {
	for _, language := range []string{"go", "python", "javascript", "typescript", "tsx", "rust"} {
		caps := Capabilities(language)
		if len(caps) != 1 || caps[0].Documentation != (language != "rust") {
			t.Errorf("documentation capability for %s: %+v", language, caps)
		}
	}
}
