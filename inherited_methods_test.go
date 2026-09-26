package codegraph

import (
	"context"
	"errors"
	"testing"
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
			g, report, err := Build(context.Background(), "inherit", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, r := range g.Relations() {
				if r.Kind != Calls {
					continue
				}
				target, _ := g.Node(r.Target)
				if target.Kind != Method {
					continue
				}
				count++
				if r.Confidence != Candidate || r.Basis != "inherited_method" || target.QualifiedName != "Base.run" && target.QualifiedName != "Base.Run" {
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
		g, _, err := Build(context.Background(), "branches", []Document{{Path: "main.py", Content: []byte(tc.source)}}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, r := range g.Relations() {
			if r.Kind == Calls {
				target, _ := g.Node(r.Target)
				if target.Kind == Method {
					if r.Basis != tc.basis || r.Confidence != Candidate {
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
	g, _, err := Build(ctx, "batch", []Document{{Path: "main.ts", Content: []byte("import {Base} from './barrel';class Child extends Base {entry(){this.run()}}")}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.AddDocuments(ctx, Document{Path: "barrel.ts", Content: []byte("export {Base} from './base'")}, Document{Path: "base.ts", Content: []byte("export class Base {run(){}}")}); err != nil {
		t.Fatal(err)
	}
	report, err := g.Wait(ctx)
	if err != nil || hasDiagnostic(report, "dynamic_call") || hasDiagnostic(report, "unresolved_type_relation") {
		t.Fatal(report, err)
	}
	count := 0
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			n, _ := g.Node(r.Target)
			if n.Kind == Method {
				count++
				if n.Location.Path != "base.ts" || r.Basis != "inherited_method" || r.Location.Path != "main.ts" {
					t.Fatal(r, n)
				}
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Relations())
	}
}

func TestInheritedMethodCyclesAndNoImplementsInheritance(t *testing.T) {
	g, report, err := Build(context.Background(), "cycle", []Document{{Path: "main.ts", Content: []byte("class A extends B {} class B extends A {} function entry(a:A){a.missing()}")}}, Options{})
	if err != nil || !hasDiagnostic(report, "dynamic_call") {
		t.Fatal(report, err)
	}
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			t.Fatal(r)
		}
	}
	g, _, err = Build(context.Background(), "implements", []Document{{Path: "main.ts", Content: []byte("class Base {run(){}} class Child implements Base {entry(){this.run()}}")}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			n, _ := g.Node(r.Target)
			if n.Kind == Method {
				t.Fatal(r)
			}
		}
	}
}

func TestInheritedMethodBudgetRollback(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "budget", []Document{{Path: "base.js", Content: []byte("export class Base {run(){}}")}}, Options{MaxRelations: 5})
	if err != nil {
		t.Fatal(err)
	}
	before := len(g.Nodes())
	if err = g.AddDocuments(ctx, Document{Path: "child.js", Content: []byte("import {Base} from './base';class Child extends Base {entry(){this.run()}}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); !errors.Is(err, ErrBuildBudget) {
		t.Fatal(err)
	}
	if len(g.Nodes()) != before {
		t.Fatal("failed batch published")
	}
}

func TestKnownGoReceiverExcludesTestMethods(t *testing.T) {
	g, _, err := Build(context.Background(), "test-methods", []Document{
		{Path: "main.go", Content: []byte("package app;type Box struct{};func Entry(b Box){b.Run()}")},
		{Path: "box_test.go", Content: []byte("package app;func (Box) Run(){}")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			t.Fatal(r)
		}
	}
}

func TestNestedInheritedReceiver(t *testing.T) {
	g, report, err := Build(context.Background(), "nested", []Document{{Path: "main.py", Content: []byte("class Base:\n    def run(self): pass\nclass Outer:\n    class Child(Base):\n        def entry(self): self.run()\n")}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range g.Relations() {
		if r.Kind == Calls {
			target, _ := g.Node(r.Target)
			if target.Kind == Method {
				count++
				source, _ := g.Node(r.Source)
				if source.QualifiedName != "Outer.Child.entry" || target.QualifiedName != "Base.run" || r.Basis != "inherited_method" {
					t.Fatal(source, target, r)
				}
			}
		}
	}
	if count != 1 {
		t.Fatal(g.Relations(), report)
	}
}
