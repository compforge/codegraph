package codegraph

import (
	"context"
	"reflect"
	"testing"
)

func TestPythonStatementFacts(t *testing.T) {
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), Document{Path: "worker.py", Content: []byte(`import sys
from lib import work as alias
path = sys.path
path.append("/opt/plugins")
if True:
    import conditional
else:
    import never
for item in ["a", "b"]:
    found = item
@decorate
def entry(arg, default=work()):
    local = arg
try:
    import wrapped
except ImportError:
    pass
`)})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, s := range facts.Statements {
		kinds = append(kinds, s.Kind)
	}
	want := []string{"import_statement", "import_from_statement", "assignment", "call", "if", "for_statement", "expression", "function_definition", "opaque"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("statement kinds = %v, want %v", kinds, want)
	}
	imports := facts.Statements[0].Imports
	if len(imports) != 1 || imports[0].Path != "sys" {
		t.Fatalf("import attachment = %+v", imports)
	}
	from := facts.Statements[1].Imports
	if len(from) != 1 || from[0].Path != "lib.work" || from[0].From != "lib" || from[0].Alias != "alias" || !reflect.DeepEqual(from[0].Names, []string{"work"}) {
		t.Fatalf("from-import attachment = %+v", from)
	}
	assignment := facts.Statements[2]
	if assignment.Target.Kind != "identifier" || assignment.Target.Text != "path" || assignment.Value.Kind != "attribute" || assignment.Value.Text != "path" {
		t.Fatalf("assignment = %+v", assignment)
	}
	conditional := facts.Statements[4]
	if len(conditional.Body) != 1 || conditional.Body[0].Kind != "import_statement" || conditional.Body[0].Imports[0].Path != "conditional" {
		t.Fatalf("if body = %+v", conditional.Body)
	}
	if len(conditional.Else) != 1 || conditional.Else[0].Imports[0].Path != "never" {
		t.Fatalf("if else = %+v", conditional.Else)
	}
	loop := facts.Statements[5]
	if loop.Target.Kind != "identifier" || loop.Target.Text != "item" || loop.Value.Kind != "list" {
		t.Fatalf("for = %+v", loop)
	}
	fn := facts.Statements[7]
	if fn.Name != "entry" || len(fn.Prelude) != 1 || fn.Prelude[0].Kind != "call" {
		t.Fatalf("function prelude = %+v", fn)
	}
	if len(fn.Body) != 1 || fn.Body[0].Kind != "assignment" {
		t.Fatalf("function body = %+v", fn.Body)
	}
	opaque := facts.Statements[8]
	if len(opaque.Body) == 0 {
		t.Fatalf("opaque body = %+v", opaque)
	}
}

func TestGoEmbedIssue(t *testing.T) {
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), Document{Path: "embed.go", Content: []byte("package app\nimport _ \"embed\"\n//go:embed static\nvar data []byte\n")})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range facts.Issues {
		if issue.Code == "unsupported_resource" {
			found = true
		}
	}
	if !found {
		t.Fatalf("go:embed not diagnosed: %+v", facts.Issues)
	}
	// The graph build path records the same issue as a build diagnostic.
	if _, err := g.AddDocuments(context.Background(), Document{Path: "embed.go", Content: []byte("package app\nimport _ \"embed\"\n//go:embed static\nvar data []byte\n")}); err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(g.Report(), "unsupported_resource") {
		t.Fatalf("build report = %+v", g.Report().Diagnostics)
	}
}

func TestImportBindingFacts(t *testing.T) {
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	py, err := g.Extract(context.Background(), Document{Path: "m.py", Content: []byte("import a.b\nfrom c import d as e\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(py.Imports) != 2 {
		t.Fatalf("imports = %+v", py.Imports)
	}
	if py.Imports[1].Binding != "d" || py.Imports[1].Alias != "e" {
		t.Fatalf("binding/alias = %+v", py.Imports[1])
	}
	goFacts, err := g.Extract(context.Background(), Document{Path: "m.go", Content: []byte("package app\nimport lib \"example.org/x/lib\"\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(goFacts.Imports) != 1 || goFacts.Imports[0].Alias != "lib" {
		t.Fatalf("go import = %+v", goFacts.Imports)
	}
}
