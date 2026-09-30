package semantics_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/compforge/codegraph"
)

// +case:id=namespace-organization,expect=`Concrete Package and Module nodes separate source contributions from recursive semantic membership`
func TestNamespaceGoPackages(t *testing.T) {
	docs := []codegraph.Document{
		{Path: "app/main.go", Content: []byte("package app\nimport alias \"example/lib\"\nfunc Run(){alias.Work()}\n")},
		{Path: "lib/type.go", Content: []byte("package library\ntype Item struct{ Value int }\n")},
		{Path: "lib/work.go", Content: []byte("package library\nfunc Work(){}\nfunc (i Item) Save(){}\n")},
		{Path: "lib/work_test.go", Content: []byte("package library_test\nfunc TestOnly(){}\n")},
		{Path: "other/a.go", Content: []byte("package library\nfunc Other(){}\n")},
	}
	g, _, err := codegraph.Build(context.Background(), "packages", docs, codegraph.Options{ModulePath: "example"})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Document {path:'app/main.go'})-[r:imports]->(p:Package) RETURN p,r`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	p := rows[0]["p"].(codegraph.Node)
	if p.Name != "library" || p.Location != nil || p.QualifiedName != "example/lib" || rows[0]["r"].(codegraph.Relation).Confidence != codegraph.Exact {
		t.Fatal(rows)
	}
	if len(query(t, g, `MATCH (:Document)-[:declares]->(p:Package {id:$id}) RETURN p`, map[string]any{"id": p.ID})) != 2 {
		t.Fatal("package must have two source contributions")
	}
	if len(query(t, g, `MATCH (:Package {id:$id})-[:contains]->(n) RETURN n`, map[string]any{"id": p.ID})) != 2 {
		t.Fatal("only direct package members; receiver methods belong to the type")
	}
	if len(query(t, g, `MATCH (:Struct {name:'Item'})-[:contains]->(:Method {name:'Save'}) RETURN 1`, nil)) != 1 {
		t.Fatal("receiver membership")
	}
	if len(query(t, g, `MATCH (:Document)-[:contains]->() RETURN 1`, nil)) != 0 {
		t.Fatal("source ownership conflated with semantic membership")
	}
	if len(query(t, g, `MATCH (:Package)-[:contains]->(:Package) RETURN 1`, nil)) != 0 {
		t.Fatal("Go directories do not nest packages")
	}
	if len(query(t, g, `MATCH (:Function {name:'Run'})-[:calls]->(:Function {name:'Work'}) RETURN 1`, nil)) != 1 {
		t.Fatal("imported calls lost")
	}
	data, _ := json.Marshal(p)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	if _, exists := obj["location"]; exists {
		t.Fatal("organization invents a location", string(data))
	}
	if len(query(t, g, `MATCH (p:Package {id:$id}) WHERE p.path IS NULL RETURN p`, map[string]any{"id": p.ID})) != 1 {
		t.Fatal("query properties invent a source path")
	}
}

func TestNamespacePythonNestingAndReload(t *testing.T) {
	ctx := context.Background()
	leaf := codegraph.Document{Path: "src/app/services/api/user.py", Content: []byte("class User:\n    def save(self): pass\n")}
	parents := []codegraph.Document{
		{Path: "src/app/__init__.py", Content: []byte("from .services.api.user import User\n")},
		{Path: "src/app/services/__init__.py", Content: []byte("")},
		{Path: "src/app/services/api/__init__.py", Content: []byte("")},
		{Path: "elsewhere/app/services/api/user.py", Content: []byte("class Other: pass\n")},
		{Path: "src/app/use.py", Content: []byte("from . import User\nimport app.services.api.user as users\ndef run():\n    return User(), users\n")},
	}
	g, _, err := codegraph.Build(ctx, "nested", []codegraph.Document{leaf}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := query(t, g, `MATCH (m:Module {name:'user'}) RETURN m`, nil)[0]["m"].(codegraph.Node)
	if err = g.AddDocuments(ctx, parents...); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	after, ok := g.Node(before.ID)
	if !ok || after.QualifiedName != "app.services.api.user" {
		t.Fatal(after)
	}
	rows := query(t, g, `MATCH (:Package {name:'app'})-[:contains]->(:Package {name:'services'})-[:contains]->(:Package {name:'api'})-[:contains]->(m:Module)-[:contains]->(:Class {name:'User'})-[:contains]->(:Method {name:'save'}) RETURN m`, nil)
	if len(rows) != 1 || rows[0]["m"].(codegraph.Node).ID != before.ID {
		t.Fatal(rows)
	}
	if len(query(t, g, `MATCH (:Function {name:'run'})-[:references]->(m:Module {id:$id}) RETURN m`, map[string]any{"id": before.ID})) != 1 {
		t.Fatal("module references must target modules")
	}
	if len(query(t, g, `MATCH (:Package {name:'app'})-[:contains]->(:Class {name:'User'}) RETURN 1`, nil)) != 0 {
		t.Fatal("re-export must not change declaration ownership")
	}
	docs := append([]codegraph.Document{leaf}, parents...)
	slices.Reverse(docs)
	full, _, err := codegraph.Build(ctx, "nested", docs, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Nodes(), full.Nodes()) || !reflect.DeepEqual(g.Relations(), full.Relations()) {
		t.Fatal("batch/order dependent graph")
	}
	assertContainsAcyclic(t, g)
}

func TestNamespaceSameNamesAndStubCandidates(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "variants", []codegraph.Document{
		{Path: "a/__init__.py", Content: []byte("")}, {Path: "b/__init__.py", Content: []byte("")},
		{Path: "a/model.py", Content: []byte("class Model: pass\n")}, {Path: "b/model.py", Content: []byte("class Model: pass\n")},
		{Path: "a/model.pyi", Content: []byte("class Model: ...\n")},
		{Path: "a/use.py", Content: []byte("from .model import Model\ndef f(): return Model\n")},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(query(t, g, `MATCH (m:Module {name:'model'}) RETURN m`, nil)) != 3 {
		t.Fatal("same names or source/stub merged")
	}
	rows := query(t, g, `MATCH (:Document {path:'a/use.py'})-[r:imports]->(m:Module {name:'model'}) RETURN r`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
			t.Fatal(r)
		}
	}
	assertContainsAcyclic(t, g)
}

func TestNamespaceBudgetsAndDetachedLocations(t *testing.T) {
	doc := codegraph.Document{Path: "a.go", Content: []byte("package p\nfunc A(){}\n")}
	for _, opts := range []codegraph.Options{{MaxNodes: 2}, {MaxRelations: 2}} {
		g, err := codegraph.New("budget", opts)
		if err != nil {
			t.Fatal(err)
		}
		if err = g.AddDocuments(context.Background(), doc); err != nil {
			t.Fatal(err)
		}
		if _, err = g.Wait(context.Background()); !errors.Is(err, codegraph.ErrBuildBudget) || len(g.Nodes()) != 0 || len(g.Relations()) != 0 {
			t.Fatal(err, g.Nodes(), g.Relations())
		}
	}
	g, _, err := codegraph.Build(context.Background(), "clone", []codegraph.Document{doc}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := g.Find("a.go", codegraph.Function, "A")[0]
	n.Location.Path = "mutated"
	if g.Find("a.go", codegraph.Function, "A")[0].Location.Path != "a.go" {
		t.Fatal("location pointer escaped")
	}
	if len(g.Find("a.go", codegraph.Package, "")) != 0 {
		t.Fatal("source lookup fabricated organization location")
	}
}

func assertContainsAcyclic(t *testing.T, g *codegraph.Graph) {
	t.Helper()
	adj := map[string][]string{}
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Contains {
			adj[r.Source] = append(adj[r.Source], r.Target)
		}
	}
	state := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		if state[id] == 1 {
			t.Fatalf("contains cycle at %s", id)
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		for _, child := range adj[id] {
			visit(child)
		}
		state[id] = 2
	}
	for id := range adj {
		visit(id)
	}
}

func TestNamespaceIsNotAClassOrCallable(t *testing.T) {
	g, report, err := codegraph.Build(context.Background(), "module-kind", []codegraph.Document{
		{Path: "pkg/api.py", Content: []byte("class Type: pass\n")},
		{Path: "pkg/use.py", Content: []byte("from . import api\nimport pkg.api as ns\nclass Bad(ns): pass\ndef use(value: ns):\n    ns()\n    return ns\n")},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, "unresolved_type_relation") {
		t.Fatal(report)
	}
	if len(query(t, g, `MATCH ()-[:extends]->(:Module) RETURN 1`, nil)) != 0 || len(query(t, g, `MATCH ()-[:calls]->(:Module) RETURN 1`, nil)) != 0 {
		t.Fatal("module mistaken for declaration target")
	}
}

func TestNamespaceUnavailableSources(t *testing.T) {
	g, report, err := codegraph.Build(context.Background(), "partial", []codegraph.Document{
		{Path: "pkg/bad.go", Content: []byte("package p\nfunc {\n")},
		{Path: "nested/plain/leaf.py", Content: []byte("from .missing import Unknown\nclass Item: pass\n")},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, "parse_error") || !hasDiagnostic(report, "unresolved_import") {
		t.Fatal(report)
	}
	if len(query(t, g, `MATCH (p:Package) RETURN p`, nil)) != 0 {
		t.Fatal("missing initializers or failed source created packages")
	}
	if len(query(t, g, `MATCH (m:Module) RETURN m`, nil)) != 1 {
		t.Fatal("unresolved import fabricated a module")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := g.Nodes()
	if err := g.AddDocuments(ctx, codegraph.Document{Path: "later/__init__.py", Content: []byte("")}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Nodes()) {
		t.Fatal("cancellation changed published organizations")
	}
}
