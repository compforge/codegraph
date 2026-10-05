package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

func TestExplicitTypeRelations(t *testing.T) {
	for _, tc := range []struct {
		path, source        string
		extends, implements int
	}{
		{"main.go", `package app; type Base interface{Run()}; type Child interface{Base}; type Box struct{};func (Box) Run(){}`, 1, 1},
		{"main.py", "class Base: pass\nclass Other: pass\nclass Child(Base, Other): pass\n", 2, 0},
		{"main.js", "class Base {}\nclass Child extends Base {}", 1, 0},
		{"main.ts", "interface Base {}\ninterface Other {}\ninterface Child extends Base, Other {}\nclass Parent {}\nclass Box extends Parent implements Base, Other {}", 3, 2},
		{"main.tsx", "interface Base {}\nclass Box implements Base {}", 0, 1},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, report, err := codegraph.Build(context.Background(), "type-relations", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			counts := map[codegraph.RelationKind]int{}
			for _, r := range g.Relations() {
				if r.Kind == codegraph.Extends || r.Kind == codegraph.Implements {
					counts[r.Kind]++
					if tc.path != "main.go" && r.Confidence != codegraph.Exact {
						t.Fatal(r)
					}
				}
			}
			if counts[codegraph.Extends] != tc.extends || counts[codegraph.Implements] != tc.implements {
				t.Fatal(counts, report)
			}
		})
	}
}

func TestImportedTypeRelationsAndIncrementalBinding(t *testing.T) {
	for _, tc := range []struct {
		path, source, basePath, base string
		confidence                   codegraph.Confidence
	}{
		{"app.go", `package app; import "example.org/lib"; type Child interface{lib.Base}`, "lib/base.go", `package lib;type Base interface{Run()}`, codegraph.Exact},
		{"app.py", "from base import Base as Parent\nclass Child(Parent): pass\n", "base.py", "class Base: pass\n", codegraph.Scoped},
		{"app.ts", "import {Parent} from './barrel';class Child extends Parent {}", "barrel.ts", "export {Base as Parent} from './base'", codegraph.Exact},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, report, err := buildTestBuilder(context.Background(), "types", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{ModulePath: "example.org"})
			if err != nil || !hasDiagnostic(report, "unresolved_type_relation") {
				t.Fatal(report, err)
			}
			docs := []codegraph.Document{{Path: tc.basePath, Content: []byte(tc.base)}}
			if tc.path == "app.ts" {
				docs = append(docs, codegraph.Document{Path: "base.ts", Content: []byte("export class Base {}")})
			}
			if err = g.AddDocuments(context.Background(), docs...); err != nil {
				t.Fatal(err)
			}
			report, err = g.Wait(context.Background())
			if err != nil || hasDiagnostic(report, "unresolved_type_relation") {
				t.Fatal(report, err)
			}
			count := 0
			for _, r := range g.Result().Relations() {
				if r.Kind == codegraph.Extends {
					count++
					if r.Confidence != tc.confidence {
						t.Fatal(r)
					}
				}
			}
			if count != 1 {
				t.Fatal(g.Result().Relations())
			}
		})
	}
}

func TestTypeRelationGapsAndDetachedFacts(t *testing.T) {
	g, report, err := buildTestBuilder(context.Background(), "gaps", []codegraph.Document{{Path: "main.py", Content: []byte("class Base: pass\nclass Child(factory(Base)): pass\nclass Missing(Unknown): pass\n")}}, codegraph.Options{})
	if err != nil || !hasDiagnostic(report, "unsupported_type_relation") || !hasDiagnostic(report, "unresolved_type_relation") {
		t.Fatal(report, err)
	}
	for _, r := range g.Result().Relations() {
		if r.Kind == codegraph.Extends {
			t.Fatal(r)
		}
	}
	doc := codegraph.Document{Path: "types.ts", Content: []byte("interface Base {}\nclass Box implements Base {}")}
	facts, err := g.Extract(context.Background(), doc)
	if err != nil || len(facts.TypeRelations) != 1 {
		t.Fatal(facts, err)
	}
	r := facts.TypeRelations[0]
	if facts.Declarations[r.Owner].Name != "Box" || r.Kind != codegraph.Implements || r.Location.StartByte == 0 {
		t.Fatal(r)
	}
	facts.TypeRelations[0].Name = "mutated"
	again, err := g.Extract(context.Background(), doc)
	if err != nil || again.TypeRelations[0].Name != "Base" {
		t.Fatal(again, err)
	}
}

func TestGoMethodSetCandidates(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "go-methods", []codegraph.Document{
		{Path: "types.go", Content: []byte(`package app;type I interface{Run(int)};type Empty interface{};type Embedded interface{I};type A struct{};type B struct{};type C struct{A}`)},
		{Path: "methods.go", Content: []byte(`package app;func (*A) Run(string){};func (B) Stop(){}`)},
		{Path: "other/types.go", Content: []byte(`package other;type I interface{Run()}`)},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Implements {
			count++
			a, _ := g.Node(r.Source)
			b, _ := g.Node(r.Target)
			if a.Name != "A" || b.Name != "I" || b.Location.Path != "types.go" || r.Confidence != codegraph.Heuristic || r.Evidence[0].Basis != "method_name_set" {
				t.Fatal(r, a, b)
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Relations())
	}
}

func TestTypeRelationsGenericsNamespaceAndShadowing(t *testing.T) {
	for _, tc := range []struct {
		path, source, dependency string
		count                    int
	}{
		{"app.ts", `import * as lib from './base';class Child extends lib.Base<string> implements lib.Contract {}`, `export class Base<T> {} export interface Contract {}`, 2},
		{"app.py", "import base as lib\nclass Child(lib.Base): pass\n", "class Base: pass\n", 1},
		{"app.js", `class Base {} function make(Base) { class Child extends Base {} }`, "", 0},
		{"app.ts", `interface Base {} function make(Base:any) { class Child implements Base {} }`, "", 0},
	} {
		t.Run(tc.path+tc.source[:8], func(t *testing.T) {
			dep := "base.ts"
			if tc.path == "app.py" {
				dep = "base.py"
			}
			docs := []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}
			if tc.dependency != "" {
				docs = append(docs, codegraph.Document{Path: dep, Content: []byte(tc.dependency)})
			}
			g, report, err := codegraph.Build(context.Background(), "types", docs, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, r := range g.Relations() {
				if r.Kind == codegraph.Extends || r.Kind == codegraph.Implements {
					count++
				}
			}
			if count != tc.count {
				t.Fatal(g.Relations(), report)
			}
		})
	}
}

func TestTypeRelationBudgetRollback(t *testing.T) {
	g, _, err := buildTestBuilder(context.Background(), "budget", []codegraph.Document{{Path: "base.ts", Content: []byte("export class Base {}")}}, codegraph.Options{MaxRelations: 8})
	if err != nil {
		t.Fatal(err)
	}
	before := len(g.Result().Nodes())
	if err = g.AddDocuments(context.Background(), codegraph.Document{Path: "child.ts", Content: []byte("import {Base} from './base';class Child extends Base {}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(context.Background()); err == nil {
		t.Fatal("expected relation budget failure")
	}
	if len(g.Result().Nodes()) != before {
		t.Fatal("failed batch published", g.Result().Nodes())
	}
}

func TestNestedTypeRelationOwner(t *testing.T) {
	g, report, err := codegraph.Build(context.Background(), "nested", []codegraph.Document{{Path: "main.py", Content: []byte("class Outer:\n    class Base: pass\n    class Child(Base): pass\n")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Extends {
			count++
			a, _ := g.Node(r.Source)
			b, _ := g.Node(r.Target)
			if a.QualifiedName != "Outer.Child" || b.QualifiedName != "Outer.Base" {
				t.Fatal(a, b, r)
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Relations(), report)
	}
}
