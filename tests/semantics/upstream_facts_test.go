package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +case:id=upstream-member-owners,expect=`Shared Go declaration ranges preserve sibling names and dotted anonymous-member owners`
func TestUpstreamGoDeclarationContainers(t *testing.T) {
	source := `package app
type Config struct {
    Server struct { Host, Port string }
    Left, Right struct { Value int }
}
var primary, secondary struct { Enabled bool }
type Alias = Config
`
	g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for child, owner := range map[string]string{
		"Config.Server.Host": "Config.Server", "Config.Server.Port": "Config.Server",
		"Config.Left.Value": "Config.Left", "Config.Right.Value": "Config.Right",
		"primary.Enabled": "primary", "secondary.Enabled": "secondary",
	} {
		nodes := g.Find("app.go", codegraph.Field, child)
		if len(nodes) != 1 {
			t.Fatalf("%s: %+v", child, g.Nodes())
		}
		node := nodes[0]
		if node.NameLocation == nil || source[node.NameLocation.StartByte:node.NameLocation.EndByte] != node.Name {
			t.Fatal(node)
		}
		for _, kind := range []codegraph.RelationKind{codegraph.Encloses, codegraph.Contains} {
			edges := g.RelationsTo(node.ID, kind)
			if len(edges) != 1 {
				t.Fatal(child, kind, edges)
			}
			parent, _ := g.Node(edges[0].Source)
			if parent.QualifiedName != owner {
				t.Fatalf("%s owner=%s want=%s", child, parent.QualifiedName, owner)
			}
		}
	}
	if len(g.Find("app.go", codegraph.TypeAlias, "Alias")) != 1 {
		t.Fatal("alias shape lost")
	}
	if len(query(t, g, `MATCH (:Field {name:'Host'})-[:encloses]->(:Field {name:'Port'}) RETURN 1`, nil)) != 0 {
		t.Fatal("sibling fields became nested")
	}
}

// +case:id=upstream-python-bindings,expect=`Module unpacking names and class fields retain source identity and bindings`
func TestUpstreamPythonDeclarationBindings(t *testing.T) {
	source := "first, second = 1, 2\nclass Settings:\n    enabled: bool = True\n\ndef use():\n    return first + second\n"
	g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: "app.py", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second", "Settings.enabled"} {
		kind := codegraph.Variable
		if name == "Settings.enabled" {
			kind = codegraph.Field
		}
		nodes := g.Find("app.py", kind, name)
		if len(nodes) != 1 {
			t.Fatalf("%s: %+v", name, g.Nodes())
		}
		node := nodes[0]
		if node.NameLocation == nil || source[node.NameLocation.StartByte:node.NameLocation.EndByte] != node.Name {
			t.Fatal(node)
		}
		if name != "Settings.enabled" && len(g.RelationsTo(node.ID, codegraph.References)) != 2 {
			t.Fatal("binding lost", node)
		}
	}
}

// +case:id=upstream-embedded-field,expect=`An embedded field retains its complete type range and the binding used for member propagation`
func TestUpstreamGoEmbeddedFieldEvidence(t *testing.T) {
	source := `package app
import "example.org/lib"
type Box struct { *actual.Base }
func Entry(box Box) { _ = box.Base.Name }
`
	g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{
		{Path: "app.go", Content: []byte(source)},
		{Path: "lib/base.go", Content: []byte("package actual; type Base struct { Name string }")},
	}, codegraph.Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	fields := g.Find("app.go", codegraph.Field, "Box.Base")
	if len(fields) != 1 {
		t.Fatal(g.Nodes())
	}
	field := fields[0]
	if source[field.Location.StartByte:field.Location.EndByte] != "*actual.Base" || field.Signature != "*actual.Base" {
		t.Fatal(field)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:references]->(:Field {name:'Name'}) RETURN r`, nil)
	if len(rows) != 1 {
		t.Fatal("embedded field type evidence lost", rows, g.Report())
	}
}

// +case:id=upstream-shared-member-docs,expect=`Anonymous members projected into sibling owners retain the same source documentation`
func TestUpstreamGoSharedMemberDocumentation(t *testing.T) {
	source := `package app
type Pair struct {
    Left, Right struct {
        // Value is shared syntax under each owner.
        // +spec=Value contract
        Value int
    }
}
`
	g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Pair.Left.Value", "Pair.Right.Value"} {
		nodes := g.Find("app.go", codegraph.Field, name)
		if len(nodes) != 1 || len(nodes[0].Documentation) != 1 || len(nodes[0].Markers) != 1 {
			t.Fatal(name, nodes)
		}
	}
}

// +case:id=upstream-alias-header,expect=`Alias headers preserve alias identity and exclude the anonymous type body`
func TestUpstreamGoAliasHeaders(t *testing.T) {
	for _, body := range []string{"struct { Value int }", "struct { Value string }", "interface { Run() }"} {
		source := "package app; type Literal = " + body
		g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		nodes := g.Find("app.go", codegraph.TypeAlias, "Literal")
		want := "Literal = struct"
		if body[0] == 'i' {
			want = "Literal = interface"
		}
		if len(nodes) != 1 || nodes[0].Signature != want {
			t.Fatal(nodes, want)
		}
	}
}

// +case:id=upstream-ts-supplemental-scopes,expect=`Adapter rules preserve nested object members, local types and const-bound lexical ownership`
func TestUpstreamTypeScriptSupplementalScopes(t *testing.T) {
	source := `type Fact = { status: string; nested: { value: number }; run(): void };
const work = () => { const startedAt = 1; return startedAt; };
function outer() { interface Local { value: number } type Token = string; }
`
	for _, path := range []string{"app.ts", "app.tsx"} {
		g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for name, kind := range map[string]codegraph.NodeKind{
			"Fact.status": codegraph.Property, "Fact.nested": codegraph.Property, "Fact.nested.value": codegraph.Property,
			"Fact.run": codegraph.Method, "work.startedAt": codegraph.Variable, "outer.Local": codegraph.Interface,
			"outer.Local.value": codegraph.Property, "outer.Token": codegraph.TypeAlias,
		} {
			if nodes := g.Find(path, kind, name); len(nodes) != 1 {
				t.Fatal(path, name, nodes)
			}
		}
	}
}

// +case:id=upstream-unbound-parameter-scope,expect=`An enclosing empty signature cannot substitute for the parameters of an unbound callable`
func TestUpstreamUnboundParameterShadowing(t *testing.T) {
	for _, path := range []string{"app.js", "app.ts", "app.tsx"} {
		source := "function target() {}\nfunction entry() { return (target) => target; }"
		g, _, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		target := g.Find(path, codegraph.Function, "target")[0]
		if edges := g.RelationsTo(target.ID, codegraph.References); len(edges) != 0 {
			t.Fatal(path, "unbound parameter leaked", edges)
		}
	}
}

// +case:id=upstream-computed-member-gap,expect=`Computed method expressions remain a coverage gap rather than a static named member`
func TestUpstreamComputedMemberGap(t *testing.T) {
	source := "class Box { [Symbol.asyncIterator]() { const value = 1; return value; } }"
	for _, path := range []string{"app.js", "app.ts", "app.tsx"} {
		g, report, err := codegraph.Build(context.Background(), "upstream", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Find(path, codegraph.Method, "")) != 0 || !hasDiagnostic(report, "unsupported_declaration") {
			t.Fatal(path, g.Nodes(), report)
		}
		if len(g.Find(path, codegraph.Variable, "Box.value")) != 1 {
			t.Fatal("computed expression changed lexical ownership")
		}
	}
}
