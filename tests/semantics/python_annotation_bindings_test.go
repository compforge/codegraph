package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +case:id=python-signature-bindings,expect=`Parameter names bind in the body; annotations and defaults retain enclosing-scope references`
func TestPythonSignatureReferences(t *testing.T) {
	cases := []struct {
		name, source string
		want         int
	}{
		{"annotation and body", "def use(value: Model):\n    return Model\n", 2},
		{"typed default", "def use(value: Model = Model):\n    return Model\n", 3},
		{"generic union and return", "def use(value: list[Model] | Model) -> Model:\n    return Model\n", 4},
		{"splat annotations", "def use(*args: Model, **kwargs: Model):\n    return Model\n", 3},
		{"own parameter", "def use(Model: Model = Model) -> Model:\n    return Model\n", 3},
		{"body assignment", "def use(value: Model = Model) -> Model:\n    Model = value\n    return Model\n", 3},
		{"bare shadow", "def use(Model):\n    return Model\n", 0},
		{"splat shadow", "def use(*Model):\n    return Model\n", 0},
		{"keyword splat shadow", "def use(**Model):\n    return Model\n", 0},
		{"outer parameter", "def outer(Model):\n    def inner(value: Model = Model) -> Model:\n        return Model\n", 0},
		{"outer annotation", "def outer(value: Model):\n    def inner(other: Model) -> Model:\n        return Model\n", 4},
		{"nested own shadow", "def outer():\n    def inner(Model: Model = Model) -> Model:\n        return Model\n", 3},
		{"lambda default", "use = lambda Model=Model: Model\n", 1},
		{"nested lambda default", "def use(value=lambda Model: Model):\n    return Model\n", 1},
		{"unrelated signature", "def other(Model: Model):\n    return Model\ndef use(value: Model):\n    return Model\n", 3},
		{"quoted annotation", "def use(value: 'Model'):\n    return Model\n", 1},
	}
	for _, imported := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if imported {
				name = "import/" + name
			}
			t.Run(name, func(t *testing.T) {
				prelude := "class Model: pass\n"
				docs := []codegraph.Document{}
				confidence := codegraph.Exact
				if imported {
					prelude = "from pkg.models import Model\n"
					confidence = codegraph.Scoped
					docs = append(docs, codegraph.Document{Path: "pkg/models.py", Content: []byte("class Model: pass\n")})
				}
				docs = append(docs, codegraph.Document{Path: "pkg/app.py", Content: []byte(prelude + tc.source)})
				g, _, err := codegraph.Build(context.Background(), "signature", docs, codegraph.Options{})
				if err != nil {
					t.Fatal(err)
				}
				rows := query(t, g, `MATCH ()-[r:references]->(:Class {name:'Model'}) RETURN r`, nil)
				if len(rows) != tc.want {
					t.Fatalf("references=%d want=%d rows=%v diagnostics=%v", len(rows), tc.want, rows, g.Report().Diagnostics)
				}
				for _, row := range rows {
					if row["r"].(codegraph.Relation).Confidence != confidence {
						t.Fatal(row)
					}
				}
			})
		}
	}
}

func TestPythonParameterBindingFacts(t *testing.T) {
	source := []byte("def use(a, b: Model, c=Model, d: Model=Model, /, *args: Model, e: Model=Model, **kwargs: Model) -> Model:\n    return a, b, c, d, args, e, kwargs\n")
	g, err := codegraph.New("facts", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "app.py", Content: source})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, ref := range facts.References {
		counts[ref.Name]++
	}
	if counts["Model"] != 9 {
		t.Fatal(counts)
	}
	for _, name := range []string{"a", "b", "c", "d", "args", "e", "kwargs"} {
		if counts[name] != 1 {
			t.Fatalf("parameter binding emitted as read: %s count=%d", name, counts[name])
		}
	}
}

func TestPythonDefaultCallsAndBodyShadowing(t *testing.T) {
	for _, source := range []string{
		"def use(target=target()):\n    target()\n",
		"def use(value=target()):\n    target = value\n    target()\n",
		"use = lambda target=target(): target()\n",
	} {
		g, _, err := codegraph.Build(context.Background(), "defaults", []codegraph.Document{
			{Path: "pkg/lib.py", Content: []byte("def target(): pass\n")},
			{Path: "pkg/app.py", Content: []byte("from pkg.lib import target\n" + source)},
		}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		rows := query(t, g, `MATCH ()-[r:calls]->(:Function {name:'target'}) RETURN r`, nil)
		if len(rows) != 1 || rows[0]["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
			t.Fatalf("source=%s calls=%v diagnostics=%v", source, rows, g.Report().Diagnostics)
		}
	}
}
