package codegraph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func sourceItem(t *testing.T, g *Graph, kind NodeKind, path, name string) Node {
	t.Helper()
	var found []Node
	for _, n := range g.Nodes() {
		if n.Kind == kind && n.Location != nil && n.Location.Path == path && n.Name == name {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s %s %s: %+v", kind, path, name, found)
	}
	return found[0]
}
func edgeTo(t *testing.T, g *Graph, from, to Node, kind RelationKind) Relation {
	t.Helper()
	for _, r := range g.RelationsFrom(from.ID, kind) {
		if r.Target == to.ID {
			return r
		}
	}
	t.Fatalf("missing %s %s -> %s: %+v", kind, from.Name, to.Name, g.RelationsFrom(from.ID))
	return Relation{}
}

// +case=Partial imports keep names; supplementation connects every source alias without renaming old nodes.
func TestSourceImportExportChain(t *testing.T) {
	ctx := context.Background()
	b, err := NewBuilder("chain", Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := Document{Path: "app.ts", Content: []byte("import { publicFoo as localFoo } from './facade'; function entry(){localFoo()}")}
	if _, err = b.addDocumentsSync(ctx, app); err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	imp := sourceItem(t, old, Import, "app.ts", "localFoo")
	use := sourceReferences(old, Calls, "localFoo")[0]
	edgeTo(t, old, use, imp, References)
	if len(old.RelationsFrom(imp.ID, Aliases, Imports)) != 0 {
		t.Fatal("missing target invented")
	}
	if imp.Binding.ImportedName != "publicFoo" || imp.Binding.LocalName != "localFoo" || imp.Binding.Specifier != "./facade" {
		t.Fatal(imp)
	}
	if string(app.Content[imp.Location.StartByte:imp.Location.EndByte]) != "publicFoo as localFoo" {
		t.Fatal(imp.Location)
	}
	facade := Document{Path: "facade.ts", Content: []byte("export { foo as publicFoo } from './impl';")}
	impl := Document{Path: "impl.ts", Content: []byte("export function foo(){}")}
	if _, err = b.addDocumentsSync(ctx, facade, impl); err != nil {
		t.Fatal(err)
	}
	g := b.Result()
	exported := sourceItem(t, g, Export, "facade.ts", "publicFoo")
	origin := sourceItem(t, g, Export, "impl.ts", "foo")
	fn := g.Find("impl.ts", Function, "foo")[0]
	edgeTo(t, g, use, imp, References)
	edgeTo(t, g, imp, exported, Aliases)
	edgeTo(t, g, exported, origin, Aliases)
	edgeTo(t, g, origin, fn, Aliases)
	caller := g.Find("app.ts", Function, "entry")[0]
	edgeTo(t, g, caller, fn, Calls)
	if len(old.RelationsFrom(imp.ID, Aliases, Imports)) != 0 {
		t.Fatal("old publication mutated")
	}
	rows := query(t, g, `MATCH (:Reference {referenceKind:'calls'})-[:references]->(i:Import)-[:aliases]->(e:Export)-[:aliases]->(o:Export)-[:aliases]->(f:Function) RETURN i,e,o,f`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	// Query and typed projections must detach binding properties too.
	projected := rows[0]["i"].(Node)
	projected.Binding.LocalName = "changed"
	original, _ := g.Node(imp.ID)
	if original.Binding.LocalName != "localFoo" {
		t.Fatal(original)
	}
	full, _, err := Build(ctx, "chain", []Document{impl, app, facade}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full.Nodes(), g.Nodes()) || !reflect.DeepEqual(full.Relations(), g.Relations()) {
		t.Fatal("input order changed graph")
	}
}

func TestSourceModuleFormsAndTypeOnly(t *testing.T) {
	source := `import './side'; import type { A as B, C } from './types'; import * as ns from './lib'; export * from './lib'; export * as publicNS from './lib'; export { B as PublicB };`
	g, _, err := Build(context.Background(), "forms", []Document{{Path: "a.ts", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := sourceItem(t, g, Import, "a.ts", "B")
	c := sourceItem(t, g, Import, "a.ts", "C")
	if b.ID == c.ID || !b.Binding.TypeOnly || !c.Binding.TypeOnly {
		t.Fatal(b, c)
	}
	rows := query(t, g, `MATCH (i:Import {typeOnly:true}) RETURN i`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	ex := sourceItem(t, g, Export, "a.ts", "PublicB")
	edgeTo(t, g, ex, b, Aliases)
	for _, n := range g.Nodes() {
		if n.Binding == nil {
			continue
		}
		if n.Binding.Form == "side_effect" && n.Binding.LocalName != "" {
			t.Fatal(n)
		}
		if n.Binding.Form == "wildcard" && (n.Binding.LocalName != "" || n.Binding.ExportedName != "" || len(g.RelationsFrom(n.ID, Aliases)) != 0) {
			t.Fatal(n)
		}
		if len(g.RelationsTo(n.ID, Declares, Encloses, Contains)) != 0 {
			t.Fatal("module item polluted declarations", n)
		}
	}
	for _, n := range g.Find("a.ts", "", "") {
		if n.Kind == Import || n.Kind == Export {
			t.Fatal(n)
		}
	}
}

func TestSourceTypeAndDecoratorRoles(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"a.py", "@route('/users')\nclass Child(Unknown):\n    pass\n"},
		{"a.ts", "@route('/users')\nclass Child extends Unknown implements Missing {}"},
		{"a.js", "@route('/users')\nclass Child extends Unknown {}"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := Build(context.Background(), "roles", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			child := g.Find(tc.path, Class, "Child")[0]
			mods := sourceReferences(g, Decorates, "route")
			if len(mods) != 1 {
				t.Fatalf("modifiers: %+v", mods)
			}
			edgeTo(t, g, mods[0], child, Decorates)
			if len(g.RelationsFrom(mods[0].ID, References)) != 0 {
				t.Fatal("decorator target invented")
			}
			bases := sourceReferences(g, Extends, "Unknown")
			if len(bases) != 1 {
				t.Fatal(bases)
			}
			if len(g.RelationsFrom(bases[0].ID, References)) != 0 {
				t.Fatal("base target invented")
			}
			if tc.path == "a.ts" && len(sourceReferences(g, Implements, "Missing")) != 1 {
				t.Fatal("lost interface role")
			}
			if !strings.HasPrefix(tc.source[mods[0].Location.StartByte:mods[0].Location.EndByte], "@route") {
				t.Fatal(mods[0])
			}
		})
	}
}

func TestSourceDecoratorBindingAndDynamicBase(t *testing.T) {
	source := "def route(x):\n    pass\n@route('x')\nclass Child(factory(Base)):\n    pass\n"
	g, _, err := Build(context.Background(), "decorator", []Document{{Path: "a.py", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mods := sourceReferences(g, Decorates, "route")
	if len(mods) != 1 {
		t.Fatal(mods)
	}
	edgeTo(t, g, mods[0], g.Find("a.py", Function, "route")[0], References)
	uses := sourceReferences(g, Extends, "factory(Base)")
	if len(uses) != 1 || len(g.RelationsFrom(uses[0].ID, References)) != 0 {
		t.Fatal(uses)
	}
}

func TestSourceItemsPreserveOpaqueSyntaxAndNestedExports(t *testing.T) {
	source := `import type Default from './types'; import type * as ns from './types'; export type { Item } from './types'; import(modulePath); export default makeValue(); namespace N { export const member = 1; }`
	g, _, err := Build(context.Background(), "syntax", []Document{{Path: "a.ts", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Default", "ns"} {
		if !sourceItem(t, g, Import, "a.ts", name).Binding.TypeOnly {
			t.Fatal(name)
		}
	}
	dynamic := sourceItem(t, g, Import, "a.ts", "modulePath")
	if dynamic.Binding.Form != "dynamic" || len(g.RelationsFrom(dynamic.ID, Imports, Aliases)) != 0 {
		t.Fatal(dynamic)
	}
	def := sourceItem(t, g, Export, "a.ts", "default")
	if def.Binding.LocalName != "" || len(g.RelationsFrom(def.ID, Aliases)) != 0 {
		t.Fatal(def)
	}
	member := sourceItem(t, g, Export, "a.ts", "member")
	ns := g.Find("a.ts", Namespace, "N")[0]
	edgeTo(t, g, ns, member, Exports)
	edgeTo(t, g, member, sourceItem(t, g, Variable, "a.ts", "member"), Aliases)
}

func TestSourcePythonImportItemLocations(t *testing.T) {
	source := "from external import first as one, second as two\nimport alpha as a, beta as b\n"
	g, _, err := Build(context.Background(), "imports", []Document{{Path: "a.py", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for local, item := range map[string]string{"one": "first as one", "two": "second as two", "a": "alpha as a", "b": "beta as b"} {
		n := sourceItem(t, g, Import, "a.py", local)
		if source[n.Location.StartByte:n.Location.EndByte] != item || n.NameLocation == nil || source[n.NameLocation.StartByte:n.NameLocation.EndByte] != local {
			t.Fatal(n)
		}
	}
}

func TestSourceDecoratorShadowing(t *testing.T) {
	docs := []Document{
		{Path: "lib.py", Content: []byte("def route(x):\n    pass\n")},
		{Path: "app.py", Content: []byte("import lib\ndef outer(lib):\n    @lib.route\n    def child():\n        pass\n")},
	}
	g, _, err := Build(context.Background(), "shadow", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	refs := sourceReferences(g, Decorates, "route")
	if len(refs) != 1 || len(g.RelationsFrom(refs[0].ID, References)) != 0 {
		t.Fatal(refs)
	}
	edgeTo(t, g, refs[0], sourceItem(t, g, Function, "app.py", "child"), Decorates)
}

func TestSourceGoImportPackageName(t *testing.T) {
	docs := []Document{
		{Path: "app.go", Content: []byte("package app\nimport (\"example/oddpath\"; ext \"external/mod\")\nfunc run(){ actual.Run(); ext.Work() }\n")},
		{Path: "oddpath/lib.go", Content: []byte("package actual\nfunc Run(){}")},
	}
	g, _, err := Build(context.Background(), "go-import", docs, Options{ModulePath: "example"})
	if err != nil {
		t.Fatal(err)
	}
	var actual, external Node
	for _, n := range g.Nodes() {
		if n.Kind == Import {
			if n.Binding.Specifier == "example/oddpath" {
				actual = n
			}
			if n.Binding.Specifier == "external/mod" {
				external = n
			}
		}
	}
	if actual.Binding == nil || actual.Binding.LocalName != "actual" || len(g.RelationsFrom(actual.ID, Aliases)) != 1 {
		t.Fatal(actual)
	}
	if external.Binding == nil || external.Binding.LocalName != "ext" || len(g.RelationsFrom(external.ID, Aliases)) != 0 {
		t.Fatal(external)
	}
}

// +case=Source module items obey node, relation and evidence limits without replacing the last publication.
func TestSourceModuleBudgets(t *testing.T) {
	ctx := context.Background()
	docs := []Document{{Path: "lib.ts", Content: []byte("export function f(){}")}, {Path: "app.ts", Content: []byte("import { f as local } from './lib'; export { local as publicName }; local()")}}
	full, report, err := Build(ctx, "budget", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, r := range full.Relations() {
		evidence += len(r.Evidence)
	}
	for name, opts := range map[string]Options{"nodes": {MaxNodes: report.Nodes - 1}, "relations": {MaxRelations: report.Relations - 1}, "evidence": {MaxEvidence: evidence - 1}} {
		t.Run(name, func(t *testing.T) {
			b, err := NewBuilder("budget", opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = b.addDocumentsSync(ctx, docs[0]); err != nil {
				t.Fatal(err)
			}
			previous := b.Result()
			if _, err = b.addDocumentsSync(ctx, docs[1]); !errors.Is(err, ErrBuildBudget) {
				t.Fatal(err)
			}
			if b.Result() != previous {
				t.Fatal("failed budget published partial graph")
			}
		})
	}
}

func TestSourceWildcardPrecedenceAndCycles(t *testing.T) {
	docs := []Document{
		{Path: "a.ts", Content: []byte("export function foo(){}; export default foo;")},
		{Path: "b.ts", Content: []byte("export function foo(){}")},
		{Path: "facade.ts", Content: []byte("export * from './a'; export {foo} from './b'; export * from './cycle';")},
		{Path: "cycle.ts", Content: []byte("export * from './facade';")},
		{Path: "app.ts", Content: []byte("import {foo} from './facade'; import absentDefault from './facade'; foo();")},
	}
	g, _, err := Build(context.Background(), "wildcard", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	imp := sourceItem(t, g, Import, "app.ts", "foo")
	ex := sourceItem(t, g, Export, "facade.ts", "foo")
	edgeTo(t, g, imp, ex, Aliases)
	edgeTo(t, g, ex, sourceItem(t, g, Export, "b.ts", "foo"), Aliases)
	if len(g.RelationsFrom(imp.ID, Aliases)) != 1 || len(g.RelationsFrom(sourceItem(t, g, Import, "app.ts", "absentDefault").ID, Aliases)) != 0 {
		t.Fatal("wildcard overrode explicit export or forwarded default")
	}
}
