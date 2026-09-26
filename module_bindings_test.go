package codegraph

import (
	"context"
	"errors"
	"testing"
)

func TestModuleSymbolBindings(t *testing.T) {
	for _, tc := range []struct {
		name, path, source, libPath, lib string
		confidence                       Confidence
	}{
		{"ts_named", "app.ts", "import {work as run} from './lib';\nexport function entry(){ return run(); }", "lib.ts", "export function work() {}", Exact},
		{"js_default", "app.js", "import run from './lib.js';\nexport function entry(){ return run(); }", "lib.js", "export default function work() {}", Exact},
		{"ts_namespace", "app.ts", "import * as lib from './lib';\nexport function entry(){ return lib.work(); }", "lib.ts", "export function work() {}", Exact},
		{"py_relative", "pkg/app.py", "from .lib import work as run\ndef entry():\n    return run()\n", "pkg/lib.py", "def work():\n    pass\n", Exact},
		{"py_absolute", "app.py", "from lib import work as run\ndef entry():\n    return run()\n", "lib.py", "def work():\n    pass\n", Candidate},
		{"py_namespace", "app.py", "import lib as helper\ndef entry():\n    return helper.work()\n", "lib.py", "def work():\n    pass\n", Candidate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := Build(context.Background(), "bindings", []Document{{Path: tc.path, Content: []byte(tc.source)}, {Path: tc.libPath, Content: []byte(tc.lib)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"calls", "references"} {
				rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:`+kind+`]->(:Function {name:'work'}) RETURN r`, nil)
				if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != tc.confidence {
					t.Fatalf("%s: %v diagnostics=%+v", kind, rows, g.Report())
				}
			}
			if tc.name != "ts_namespace" && tc.name != "py_namespace" {
				rows := query(t, g, `MATCH (:Document)-[r:imports]->(:Function {name:'work'}) RETURN r`, nil)
				if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != tc.confidence {
					t.Fatal(rows)
				}
			}
		})
	}
}

func TestModuleReExportChains(t *testing.T) {
	for _, forward := range []string{
		"export {work as publicWork} from './lib';",
		"import {work as local} from './lib'; export {local as publicWork};",
		"export * from './middle';",
	} {
		t.Run(forward, func(t *testing.T) {
			g, _, err := Build(context.Background(), "reexport", []Document{
				{Path: "app.ts", Content: []byte("import {publicWork as run} from './barrel'; export function entry(){ return run(); }")},
				{Path: "barrel.ts", Content: []byte(forward)},
				{Path: "middle.ts", Content: []byte("export {work as publicWork} from './lib';")},
				{Path: "lib.ts", Content: []byte("export function work() {}")},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(:Function {name:'work'}) RETURN r`, nil)
			if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != Exact {
				t.Fatal(rows, g.Report())
			}
		})
	}
}

func TestModuleBindingShadowingAndVisibility(t *testing.T) {
	for _, tc := range []struct{ path, source, libPath, lib string }{
		{"app.ts", "import {work as run} from './lib'; function entry(run: () => void){ run(); }", "lib.ts", "export function work() {}"},
		{"app.ts", "import * as lib from './lib'; function entry(lib: any){ lib.work(); }", "lib.ts", "export function work() {}"},
		{"pkg/app.py", "from .lib import work as run\ndef entry(run):\n    return run()\n", "pkg/lib.py", "def work():\n    pass\n"},
		{"app.ts", "import {work} from './lib'; function entry(){ work(); }", "lib.ts", "function work() {}"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			g, _, err := Build(context.Background(), "shadow", []Document{{Path: tc.path, Content: []byte(tc.source)}, {Path: tc.libPath, Content: []byte(tc.lib)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'entry'})-[r]->(:Function {name:'work'}) RETURN r`, nil)
			if len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
}

func TestModuleBindingCandidatesCyclesAndIncremental(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "app.ts", Content: []byte("import {work} from './lib'; function entry(){ work(); }")}
	g, report, err := Build(ctx, "batch", []Document{doc}, Options{})
	if err != nil || !hasDiagnostic(report, "unresolved_import_binding") {
		t.Fatal(report, err)
	}
	if err = g.AddDocuments(ctx, Document{Path: "lib.ts", Content: []byte("export function work() {}")}, Document{Path: "lib.js", Content: []byte("export function work() {}")}); err != nil {
		t.Fatal(err)
	}
	report, err = g.Wait(ctx)
	if err != nil || hasDiagnostic(report, "unresolved_import_binding") {
		t.Fatal(report, err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(:Function {name:'work'}) RETURN r`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Candidate {
			t.Fatal(row)
		}
	}
	_, report, err = Build(ctx, "cycle", []Document{
		{Path: "app.ts", Content: []byte("import {work} from './a'; function entry(){ work(); }")},
		{Path: "a.ts", Content: []byte("export {work} from './b';")},
		{Path: "b.ts", Content: []byte("export {work} from './a';")},
	}, Options{})
	if err != nil || !hasDiagnostic(report, "unresolved_import_binding") {
		t.Fatal(report, err)
	}
	limited, err := New("budget", Options{MaxRelations: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(ctx, doc, Document{Path: "lib.ts", Content: []byte("export function work() {}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(ctx); !errors.Is(err, ErrBuildBudget) || len(limited.Nodes()) != 0 {
		t.Fatal(err, limited.Nodes())
	}
}

func TestImportBindingFactsDetached(t *testing.T) {
	ctx := context.Background()
	g, err := New("facts", Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "app.ts", Content: []byte("import dflt, {work as run} from './lib'; export {run as publicRun};")}
	facts, err := g.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Imports) != 1 || len(facts.Imports[0].Bindings) != 2 || facts.Exports["publicRun"] != "run" {
		t.Fatal(facts)
	}
	names := map[string]string{}
	for _, binding := range facts.Imports[0].Bindings {
		names[binding.Local] = binding.Name
	}
	if names["dflt"] != "default" || names["run"] != "work" {
		t.Fatal(names)
	}
	facts.Imports[0].Bindings[0].Local = "mutated"
	again, err := g.Extract(ctx, doc)
	if err != nil || again.Imports[0].Bindings[0].Local == "mutated" {
		t.Fatal(again, err)
	}
}

func TestExplicitExportOverridesStarAndPreservesNamespace(t *testing.T) {
	g, _, err := Build(context.Background(), "override", []Document{
		{Path: "app.ts", Content: []byte("import * as api from './barrel'; function entry(){ api.work(); }")},
		{Path: "barrel.ts", Content: []byte("export * from './lib'; export function work() {}")},
		{Path: "lib.ts", Content: []byte("export function work() {}")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(target:Function) RETURN r,target`, nil)
	if len(rows) != 1 || rows[0]["target"].(Node).Location.Path != "barrel.ts" || rows[0]["r"].(Relation).Confidence != Exact {
		t.Fatal(rows)
	}
	rows = query(t, g, `MATCH (:Function {name:'entry'})-[:references]->(target:Document) RETURN target`, nil)
	if len(rows) != 1 || rows[0]["target"].(Node).Location.Path != "barrel.ts" {
		t.Fatal(rows)
	}
}

func TestPythonScopedImportAndReExport(t *testing.T) {
	g, _, err := Build(context.Background(), "python", []Document{
		{Path: "pkg/app.py", Content: []byte("def entry():\n    from .barrel import public as run\n    return run()\ndef unrelated():\n    return run()\n")},
		{Path: "pkg/barrel.py", Content: []byte("from .lib import work as public\n")},
		{Path: "pkg/lib.py", Content: []byte("def work():\n    pass\n")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(:Function {name:'work'}) RETURN r`, nil)
	if len(rows) != 1 {
		t.Fatal(rows, g.Report())
	}
	rows = query(t, g, `MATCH (:Function {name:'unrelated'})-[:calls]->(:Function {name:'work'}) RETURN 1`, nil)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestStarDoesNotForwardDefaultOrNamespaceMembers(t *testing.T) {
	for _, forward := range []string{"export * from './lib';", "export * as api from './lib';"} {
		g, report, err := Build(context.Background(), "limits", []Document{
			{Path: "app.ts", Content: []byte("import run from './barrel'; import {work} from './barrel'; function entry(){ run(); work(); }")},
			{Path: "barrel.ts", Content: []byte(forward)},
			{Path: "lib.ts", Content: []byte("export default function defaultWork() {} export function work() {}")},
		}, Options{})
		if err != nil || !hasDiagnostic(report, "unresolved_import_binding") {
			t.Fatal(report, err)
		}
		rows := query(t, g, `MATCH (:Function {name:'entry'})-[:calls]->(:Function {name:'defaultWork'}) RETURN 1`, nil)
		if len(rows) != 0 {
			t.Fatal("star must not forward default", rows)
		}
		if forward == "export * as api from './lib';" {
			rows = query(t, g, `MATCH (:Function {name:'entry'})-[:calls]->(:Function {name:'work'}) RETURN 1`, nil)
			if len(rows) != 0 {
				t.Fatal("namespace export must not flatten members", rows)
			}
		}
	}
}
