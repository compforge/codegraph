package semantics_test

import (
	"context"
	"reflect"
	"testing"

	cg "github.com/compforge/codegraph"
)

func ancestorNames(t *testing.T, g *cg.Graph, id string, opts cg.NamespaceOptions) []string {
	t.Helper()
	matches, err := g.NamespaceAncestors(context.Background(), id, opts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range matches {
		names = append(names, m.Node.QualifiedName)
	}
	return names
}

func TestGoModuleOrganizationAndNavigation(t *testing.T) {
	ctx := context.Background()
	docs := []cg.Document{
		{Path: "app/a.go", Content: []byte("package app\nimport \"example.org/root/lib\"\nfunc Run(){lib.Work()}\n")},
		{Path: "lib/a.go", Content: []byte("package lib\ntype Item struct{}\nfunc Work(){}\n")},
		{Path: "lib/method.go", Content: []byte("package lib\nfunc (Item) Save(){}\n")},
		{Path: "nested/lib/a.go", Content: []byte("package lib\nfunc Work(){}\n")},
		{Path: "nested/deeper/a.go", Content: []byte("package deep\n")},
	}
	opts := cg.Options{ModulePath: "example.org/root", ResolutionContext: cg.ResolutionContext{GoModules: map[string]string{"nested": "example.org/child", "unused": "example.org/unused"}}}
	builder, _, err := buildTestBuilder(ctx, "modules", docs[:1], opts)
	if err != nil {
		t.Fatal(err)
	}
	old := builder.Result()
	oldNodes, oldRelations := old.Nodes(), old.Relations()
	if err := builder.AddDocuments(ctx, docs[1:]...); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	g := builder.Result()
	if !reflect.DeepEqual(oldNodes, old.Nodes()) || !reflect.DeepEqual(oldRelations, old.Relations()) {
		t.Fatal("published graph mutated")
	}
	for _, n := range oldNodes {
		if _, ok := g.Node(n.ID); !ok {
			t.Fatal("identity changed", n)
		}
	}
	full, _, err := cg.Build(ctx, "modules", docs, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full.Nodes(), g.Nodes()) || !reflect.DeepEqual(full.Relations(), g.Relations()) {
		t.Fatal("incremental graph differs")
	}
	for path, want := range map[string][]string{
		"app/a.go":           {"example.org/root/app", "example.org/root"},
		"nested/lib/a.go":    {"example.org/child/lib", "example.org/child"},
		"nested/deeper/a.go": {"example.org/child/deeper", "example.org/child"},
	} {
		if got := ancestorNames(t, g, cg.DocumentID(path), cg.NamespaceOptions{}); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %v, want %v", path, got, want)
		}
	}
	common, err := g.CommonNamespaces(ctx, []string{cg.DocumentID("app/a.go"), cg.DocumentID("lib/a.go")}, cg.NamespaceOptions{})
	if err != nil || len(common) != 1 || common[0].Node.Kind != cg.Module || common[0].Node.Name != "example.org/root" || common[0].Depth != 2 {
		t.Fatal(common, err)
	}
	common, err = g.CommonNamespaces(ctx, []string{cg.DocumentID("app/a.go"), cg.DocumentID("nested/lib/a.go")}, cg.NamespaceOptions{})
	if err != nil || len(common) != 0 {
		t.Fatal("nested module must not inherit outer module", common, err)
	}
	for _, n := range g.Nodes() {
		if n.Kind == cg.Module {
			if n.Location != nil || n.Name == "example.org/unused" || len(g.RelationsTo(n.ID, cg.Declares, cg.Contains)) != 0 {
				t.Fatal("invented module source or hierarchy", n)
			}
		}
		if n.Kind == cg.Import || n.Kind == cg.Reference && n.Location.Path == "app/a.go" {
			if got := ancestorNames(t, g, n.ID, cg.NamespaceOptions{}); !reflect.DeepEqual(got, []string{"example.org/root/app", "example.org/root"}) {
				t.Fatal("source ownership followed target", n, got)
			}
		}
	}
	method := g.Find("lib/method.go", cg.Method, "")[0]
	got, err := g.NamespaceAncestors(ctx, method.ID, cg.NamespaceOptions{})
	if err != nil || len(got) != 3 || got[0].Node.Kind != cg.Struct || got[1].Node.Kind != cg.Package || got[2].Node.Kind != cg.Module {
		t.Fatal(got, err)
	}
	for _, r := range g.RelationsFrom(commonModuleID(t, g), cg.Contains) {
		if r.Confidence != cg.Exact || len(r.Evidence) != 1 || r.Evidence[0].Basis != "module_context" {
			t.Fatal(r)
		}
	}
	without, _, err := cg.Build(ctx, "no-context", docs[:1], cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ancestorNames(t, without, cg.DocumentID("app/a.go"), cg.NamespaceOptions{}); !reflect.DeepEqual(got, []string{"app:app"}) {
		t.Fatal(got)
	}
}

func commonModuleID(t *testing.T, g *cg.Graph) string {
	t.Helper()
	for _, n := range g.Nodes() {
		if n.Kind == cg.Module && n.Name == "example.org/root" {
			return n.ID
		}
	}
	t.Fatal("module missing")
	return ""
}

func TestPythonNamespaceQueriesAfterSupplement(t *testing.T) {
	ctx := context.Background()
	leaf := cg.Document{Path: "app/api/user.py", Content: []byte("class User:\n    def save(self): pass\n")}
	b, _, err := buildTestBuilder(ctx, "python-navigation", []cg.Document{leaf}, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	id := cg.DocumentID(leaf.Path)
	before := ancestorNames(t, old, id, cg.NamespaceOptions{})
	if len(before) != 1 {
		t.Fatal(before)
	}
	if err := b.AddDocuments(ctx, cg.Document{Path: "app/__init__.py", Content: []byte("")}, cg.Document{Path: "app/api/__init__.py", Content: []byte("")}, cg.Document{Path: "app/api/item.py", Content: []byte("class Item: pass\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := ancestorNames(t, b.Result(), id, cg.NamespaceOptions{}); !reflect.DeepEqual(got, []string{"app.api.user", "app.api", "app"}) {
		t.Fatal(got)
	}
	if got := ancestorNames(t, old, id, cg.NamespaceOptions{}); !reflect.DeepEqual(got, before) {
		t.Fatal("old graph changed", got)
	}
	common, err := b.Result().CommonNamespaces(ctx, []string{id, cg.DocumentID("app/api/item.py")}, cg.NamespaceOptions{})
	if err != nil || len(common) != 2 || common[0].Node.QualifiedName != "app.api" || common[1].Node.QualifiedName != "app" {
		t.Fatal(common, err)
	}
}

func TestTypeScriptNamespaceQueries(t *testing.T) {
	ctx := context.Background()
	g, _, err := cg.Build(ctx, "ts-navigation", []cg.Document{
		{Path: "src/a.ts", Content: []byte("export namespace API { export class Service { run() {} } }")},
		{Path: "src/b.ts", Content: []byte("export function work() {}")},
	}, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	method := g.Find("src/a.ts", cg.Method, "")[0]
	matches, err := g.NamespaceAncestors(ctx, method.ID, cg.NamespaceOptions{})
	if err != nil || len(matches) != 3 || matches[0].Node.Kind != cg.Class || matches[1].Node.Kind != cg.Namespace || matches[2].Node.Kind != cg.Module {
		t.Fatal(matches, err)
	}
	filtered, err := g.NamespaceAncestors(ctx, method.ID, cg.NamespaceOptions{Kinds: []cg.NodeKind{cg.Module}})
	if err != nil || len(filtered) != 1 || filtered[0].Depth != 3 {
		t.Fatal(filtered, err)
	}
	common, err := g.CommonNamespaces(ctx, []string{cg.DocumentID("src/a.ts"), cg.DocumentID("src/b.ts")}, cg.NamespaceOptions{})
	if err != nil || len(common) != 0 {
		t.Fatal("directory must not become namespace", common, err)
	}
}

func TestGoModuleContextReorganizesStablePackages(t *testing.T) {
	ctx := context.Background()
	docs := []cg.Document{{Path: "a/lib/x.go", Content: []byte("package lib\nfunc X(){}\n")}}
	b, _, err := buildTestBuilder(ctx, "context-navigation", docs, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := b.Result()
	old, err := before.NamespaceAncestors(ctx, cg.DocumentID(docs[0].Path), cg.NamespaceOptions{})
	if err != nil || len(old) != 1 {
		t.Fatal(old, err)
	}
	if err := b.SetResolutionContext(cg.ResolutionContext{GoModules: map[string]string{".": "example.org/root", "a": "example.org/child"}}); err != nil {
		t.Fatal(err)
	}
	after, _, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := after.NamespaceAncestors(ctx, cg.DocumentID(docs[0].Path), cg.NamespaceOptions{})
	if err != nil || len(got) != 2 || got[0].Node.ID != old[0].Node.ID || got[0].Node.QualifiedName != "example.org/child/lib" || got[1].Node.QualifiedName != "example.org/child" {
		t.Fatal(got, err)
	}
	if node, _ := before.Node(old[0].Node.ID); node.QualifiedName != "a/lib:lib" {
		t.Fatal("old publication changed", node)
	}
	if err := b.SetResolutionContext(cg.ResolutionContext{}); err != nil {
		t.Fatal(err)
	}
	restored, _, err := b.Build(ctx)
	if err != nil || !reflect.DeepEqual(restored.Nodes(), before.Nodes()) || !reflect.DeepEqual(restored.Relations(), before.Relations()) {
		t.Fatal("obsolete context retained", err)
	}
}
