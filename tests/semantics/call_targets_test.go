package semantics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/compforge/codegraph"
)

func TestGoReceiverAndCallableCandidates(t *testing.T) {
	ctx := context.Background()
	source := `package app
 type Box struct{}
 func (b *Box) Run(){}
 func Work(){}
 func Entry(b *Box){ b.Run(); x:=Box{}; x.Run(); cb:=Work; cb(); method:=b.Run; method() }
 `
	g, _, err := buildTestBuilder(ctx, "go-calls", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[r:calls]->(target) RETURN r,target`, nil)
	if len(rows) != 4 {
		t.Fatal(rows, g.Report())
	}
	counts := map[string]int{}
	for _, row := range rows {
		edge := row["r"].(codegraph.Relation)
		if edge.Confidence != codegraph.Scoped {
			t.Fatal(edge)
		}
		counts[row["target"].(codegraph.Node).Name]++
	}
	if counts["Run"] != 3 || counts["Work"] != 1 {
		t.Fatal(counts)
	}
	facts, err := g.Extract(ctx, codegraph.Document{Path: "app.go", Content: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, call := range facts.Calls {
		if len(call.Targets) > 0 {
			count++
		}
	}
	if count != 4 {
		t.Fatal(facts.Calls)
	}
	facts.Calls[0].Targets[0].Name = "mutated"
	again, err := g.Extract(ctx, codegraph.Document{Path: "app.go", Content: []byte(source)})
	if err != nil || again.Calls[0].Targets[0].Name == "mutated" {
		t.Fatal(again, err)
	}
}

func TestGoCrossFileImportedReceiver(t *testing.T) {
	ctx := context.Background()
	g, report, err := buildTestBuilder(ctx, "go-batch", []codegraph.Document{{Path: "app.go", Content: []byte("package app\nimport lib \"example.org/lib\"\nfunc Entry(box *lib.Box){box.Run()}\n")}}, codegraph.Options{ModulePath: "example.org"})
	if err != nil || !hasDiagnostic(report, "dynamic_call") {
		t.Fatal(report, err)
	}
	if err = g.AddDocuments(ctx, codegraph.Document{Path: "lib/type.go", Content: []byte("package lib\ntype Box struct{}\n")}, codegraph.Document{Path: "lib/method.go", Content: []byte("package lib\nfunc (b *Box) Run(){}\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	rows := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[r:calls]->(m:Method {name:'Run'}) RETURN r,m`, nil)
	if len(rows) != 1 || rows[0]["r"].(codegraph.Relation).Confidence != codegraph.Scoped || rows[0]["m"].(codegraph.Node).Location.Path != "lib/method.go" {
		t.Fatal(rows, g.Report())
	}
}

func TestModuleReceiverCandidates(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.py", "class Box:\n    def run(self):\n        pass\n    def entry(self):\n        self.run()\n"},
		{"app.ts", "class Box { run() {} entry() { this.run(); } }"},
		{"app.js", "class Box { run() {} entry() { this.run(); } }"},
		{"app.ts", "class Box { run() {} } function entry(box: Box) { box.run(); }"},
		{"app.js", "class Box { run() {} } function entry() { const box=new Box(); box.run(); }"},
		{"app.py", "class Box:\n    def run(self):\n        pass\ndef entry():\n    box=Box()\n    box.run()\n"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			g, _, err := codegraph.Build(context.Background(), "module-calls", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (source {name:'entry'})-[r:calls]->(target:Method {name:'run'}) RETURN r,target`, nil)
			if len(rows) != 1 || rows[0]["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
				t.Fatal(rows, g.Report())
			}
		})
	}
}

func TestImportedClassConstructorAndMethod(t *testing.T) {
	for _, tc := range []struct{ path, source, libPath, lib string }{
		{"app.ts", "import {Box as Worker} from './lib'; function entry(){const box=new Worker();box.run();}", "lib.ts", "export class Box { run() {} }"},
		{"pkg/app.py", "from .lib import Box as Worker\ndef entry():\n    box=Worker()\n    box.run()\n", "pkg/lib.py", "class Box:\n    def run(self):\n        pass\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := codegraph.Build(context.Background(), "class", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}, {Path: tc.libPath, Content: []byte(tc.lib)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (source)-[r:calls]->(target) RETURN r,target`, nil)
			if len(rows) != 2 {
				t.Fatal(rows, g.Report())
			}
			for _, row := range rows {
				if row["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
					t.Fatal(row)
				}
			}
		})
	}
}

func TestUnknownReceiverRetainsCandidatesAndIsolation(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "candidates", []codegraph.Document{
		{Path: "app.ts", Content: []byte("class A {run(){}} class B {run(){}} function entry(obj: unknown){obj.run();}")},
		{Path: "other.py", Content: []byte("class Other:\n    def run(self):\n        pass\n")},
	}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(target:Method) RETURN r,target`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(codegraph.Relation).Confidence != codegraph.NameOnly || row["target"].(codegraph.Node).Location.Path != "app.ts" {
			t.Fatal(row)
		}
	}
}

func TestCallTargetsPreserveClosureAndCallbackGaps(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package app\ntype Box struct{}\nfunc (b *Box) Run(){}\nfunc Work(){}\nfunc Entry(Work func(),b *Box){ Work(); _ = func(){b.Run()} }"},
		{"app.ts", "class Box {run(){}} function entry(box: Box){return ()=>box.run();}"},
		{"app.py", "class Box:\n    def run(self):\n        pass\ndef entry(box: Box):\n    return lambda: box.run()\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, report, err := buildTestBuilder(context.Background(), "gap", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil || !hasDiagnostic(report, "dynamic_call") {
				t.Fatal(report, err)
			}
			rows := query(t, g.Result(), `MATCH (source)-[:calls]->(target) RETURN source,target`, nil)
			if len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
	g, err := codegraph.NewBuilder("budget", codegraph.Options{MaxRelations: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.AddDocuments(context.Background(), codegraph.Document{Path: "app.ts", Content: []byte("class Box {run(){} entry(){this.run();}}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(context.Background()); !errors.Is(err, codegraph.ErrBuildBudget) || len(g.Result().Nodes()) != 0 {
		t.Fatal(err, g.Result().Nodes())
	}
}

func TestGoCallableReassignmentCandidates(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "reassigned", []codegraph.Document{{Path: "app.go", Content: []byte("package app\nfunc A(){}\nfunc B(){}\nfunc Entry(){ cb:=A; cb=B; cb() }\n")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:calls]->(target:Function) RETURN r,target`, nil)
	if len(rows) != 2 {
		t.Fatal(rows, g.Report())
	}
	for _, row := range rows {
		if row["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
			t.Fatal(row)
		}
	}
}

func TestConstructorShadowing(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.ts", "class Box{} function entry(Box:any){new Box();}"},
		{"app.py", "class Box:\n    pass\ndef entry(Box):\n    return Box()\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := codegraph.Build(context.Background(), "shadowed-class", []codegraph.Document{{Path: tc.path, Content: []byte(tc.source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'entry'})-[:calls]->(:Class {name:'Box'}) RETURN 1`, nil)
			if len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
}

func TestGoMethodExpressionsAndUnqualifiedPackageName(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "expression", []codegraph.Document{
		{Path: "app.go", Content: []byte("package app\nimport \"example.org/worker\"\ntype Box struct{}\nfunc (b Box) Run(){}\nfunc Entry(){Box.Run(Box{})}\nfunc Imported(b *actual.Box){b.Run()}\n")},
		{Path: "worker/box.go", Content: []byte("package actual\ntype Box struct{}\nfunc (b *Box) Run(){}\n")},
	}, codegraph.Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Entry", "Imported"} {
		rows := query(t, g, `MATCH (:Function {name:$name})-[r:calls]->(:Method {name:'Run'}) RETURN r`, map[string]any{"name": name})
		if len(rows) != 1 || rows[0]["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
			t.Fatal(name, rows, g.Report())
		}
	}
}

func TestGoUnknownReceiverMethodCandidates(t *testing.T) {
	g, _, err := codegraph.Build(context.Background(), "unknown-method", []codegraph.Document{{Path: "app.go", Content: []byte("package app\ntype A struct{}\ntype B struct{}\nfunc (a A) Run(){}\nfunc (b B) Run(){}\nfunc Entry(x interface{Run()}){x.Run()}\n")}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:calls]->(:Method {name:'Run'}) RETURN r`, nil)
	if len(rows) != 2 {
		t.Fatal(rows, g.Report())
	}
	for _, row := range rows {
		if row["r"].(codegraph.Relation).Confidence != codegraph.NameOnly || row["r"].(codegraph.Relation).Evidence[0].Basis != "method_name" {
			t.Fatal(row)
		}
	}
}
