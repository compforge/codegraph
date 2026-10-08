package semantics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/compforge/codegraph"
)

func TestInheritedMethodCalls(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"main.go", `package app;type Base interface{Run()};type Mid interface{Base};type Child interface{Mid};func Entry(c Child){c.Run()}`},
		{"main.py", "class Base:\n    def run(self): pass\nclass Mid(Base): pass\nclass Child(Mid):\n    def entry(self): self.run()\n"},
		{"main.js", "class Base {run(){}} class Mid extends Base {} class Child extends Mid {entry(){this.run()}}"},
		{"main.ts", "class Base {run(){}} class Mid extends Base {} class Child extends Mid {} function entry(c:Child){c.run()}"},
		{"main.tsx", "class Base {run(){}} class Child extends Base {entry(){this.run()}}"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, report, err := codegraph.Build(context.Background(), "inherit", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, r := range g.Relations() {
				if r.Kind != codegraph.Calls {
					continue
				}
				target, _ := g.Node(r.Target)
				if target.Kind != codegraph.Method {
					continue
				}
				count++
				if r.Confidence != codegraph.Scoped || r.Evidence[0].Basis != "inherited_method" || target.QualifiedName != "Base.run" && target.QualifiedName != "Base.Run" {
					t.Fatal(r, target)
				}
			}
			if count != 1 || hasDiagnostic(report, "dynamic_call") {
				t.Fatal(g.Relations(), report)
			}
		})
	}
}

func TestInheritedMethodOverrideAndMultipleBases(t *testing.T) {
	for _, tc := range []struct {
		source  string
		targets []string
		basis   string
	}{
		{"class Base:\n    def run(self): pass\nclass Child(Base):\n    def run(self): pass\n    def entry(self): self.run()\n", []string{"Child.run"}, "lexical_receiver"},
		{"class A:\n    def run(self): pass\nclass B:\n    def run(self): pass\nclass Child(A,B):\n    def entry(self): self.run()\n", []string{"A.run", "B.run"}, "inherited_method"},
		{"class Base:\n    def run(self): pass\nclass A(Base): pass\nclass B(Base): pass\nclass Child(A,B):\n    def entry(self): self.run()\n", []string{"Base.run"}, "inherited_method"},
	} {
		g, _, err := codegraph.Build(context.Background(), "branches", []codegraph.Document{{Path: "main.py", Content: []byte(tc.source)}}, codegraph.Options{})
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, r := range g.Relations() {
			if r.Kind == codegraph.Calls {
				target, _ := g.Node(r.Target)
				if target.Kind == codegraph.Method {
					if r.Evidence[0].Basis != tc.basis || r.Confidence != codegraph.Scoped {
						t.Fatal(r)
					}
					found[target.QualifiedName] = true
				}
			}
		}
		if len(found) != len(tc.targets) {
			t.Fatal(found, g.Report())
		}
		for _, target := range tc.targets {
			if !found[target] {
				t.Fatal(found)
			}
		}
	}
}

func TestInheritedMethodImportedBaseIncremental(t *testing.T) {
	ctx := context.Background()
	g, _, err := buildTestBuilder(ctx, "batch", []codegraph.Document{{Path: "main.ts", Content: []byte("import {Base} from './barrel';class Child extends Base {entry(){this.run()}}")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.AddDocuments(ctx, codegraph.Document{Path: "barrel.ts", Content: []byte("export {Base} from './base'")}, codegraph.Document{Path: "base.ts", Content: []byte("export class Base {run(){}}")}); err != nil {
		t.Fatal(err)
	}
	report, err := g.Wait(ctx)
	if err != nil || hasDiagnostic(report, "dynamic_call") || hasDiagnostic(report, "unresolved_type_relation") {
		t.Fatal(report, err)
	}
	count := 0
	for _, r := range g.Result().Relations() {
		if r.Kind == codegraph.Calls {
			n, _ := g.Result().Node(r.Target)
			if n.Kind == codegraph.Method {
				count++
				if n.Location.Path != "base.ts" || r.Evidence[0].Basis != "inherited_method" || r.Location.Path != "main.ts" {
					t.Fatal(r, n)
				}
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Result().Relations())
	}
}

func TestInheritedMethodCyclesAndNoImplementsInheritance(t *testing.T) {
	g, report, err := codegraph.Build(context.Background(), "cycle", []codegraph.Document{{Path: "main.ts", Content: []byte("class A extends B {} class B extends A {} function entry(a:A){a.missing()}")}}, codegraph.Options{})
	if err != nil || !hasDiagnostic(report, "dynamic_call") {
		t.Fatal(report, err)
	}
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Calls {
			t.Fatal(r)
		}
	}
	g, _, err = codegraph.Build(context.Background(), "implements", []codegraph.Document{{Path: "main.ts", Content: []byte("class Base {run(){}} class Child implements Base {entry(){this.run()}}")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Calls {
			n, _ := g.Node(r.Target)
			if n.Kind == codegraph.Method {
				t.Fatal(r)
			}
		}
	}
}

func TestInheritedMethodBudgetRollback(t *testing.T) {
	ctx := context.Background()
	g, _, err := buildTestBuilder(ctx, "budget", []codegraph.Document{{Path: "base.js", Content: []byte("export class Base {run(){}}")}}, codegraph.Options{MaxRelations: 12})
	if err != nil {
		t.Fatal(err)
	}
	before := len(g.Result().Nodes())
	if err = g.AddDocuments(ctx, codegraph.Document{Path: "child.js", Content: []byte("import {Base} from './base';class Child extends Base {entry(){this.run()}}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); !errors.Is(err, codegraph.ErrBuildBudget) {
		t.Fatal(err)
	}
	if len(g.Result().Nodes()) != before {
		t.Fatal("failed batch published")
	}
}

func TestKnownGoReceiverExcludesTestMethods(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "test-methods", []codegraph.Document{
		{Path: "main.go", Content: []byte("package app;type Box struct{};func Entry(b Box){b.Run()}")},
		{Path: "box_test.go", Content: []byte("package app;func (Box) Run(){}")},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Calls {
			t.Fatal(r)
		}
	}
}

func TestNestedInheritedReceiver(t *testing.T) {
	g, report, err := codegraph.Build(context.Background(), "nested", []codegraph.Document{{Path: "main.py", Content: []byte("class Base:\n    def run(self): pass\nclass Outer:\n    class Child(Base):\n        def entry(self): self.run()\n")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range g.Relations() {
		if r.Kind == codegraph.Calls {
			target, _ := g.Node(r.Target)
			if target.Kind == codegraph.Method {
				count++
				source, _ := g.Node(r.Source)
				if source.QualifiedName != "Outer.Child.entry" || target.QualifiedName != "Base.run" || r.Evidence[0].Basis != "inherited_method" {
					t.Fatal(source, target, r)
				}
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Relations(), report)
	}
}
