package codegraph

import (
	"context"
	"testing"
)

func TestReferenceFactsAndRelations(t *testing.T) {
	ctx := context.Background()
	source := []byte("package app\nconst Limit = 3\nfunc target() {}\nfunc use() { _ = Limit; _ = Limit; callback := target; callback() }\n")
	g, err := New("references", Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(ctx, Document{Path: "app.go", Content: source})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range facts.References {
		counts[r.Name]++
		if string(source[r.Location.StartByte:r.Location.EndByte]) != r.Name {
			t.Fatalf("wrong position: %+v", r)
		}
		if r.Owner < 0 || facts.Declarations[r.Owner].Name != "use" {
			t.Fatalf("wrong owner: %+v", r)
		}
	}
	if counts["Limit"] != 2 || counts["target"] != 1 || counts["callback"] != 1 {
		t.Fatal(counts)
	}
	if err = g.AddDocuments(ctx, Document{Path: "app.go", Content: source}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (a:Function {name:'use'})-[r:references]->(b) RETURN r,b`, nil)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Exact {
			t.Fatal(row)
		}
	}
	// Detached extraction results must not mutate retained facts.
	facts.References[0].Name = "changed"
	again, err := g.Extract(ctx, Document{Path: "app.go", Content: source})
	if err != nil || again.References[0].Name == "changed" {
		t.Fatal(again, err)
	}
}

func TestReferenceCandidatesAndUnresolved(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "candidates", []Document{
		{Path: "use.go", Content: []byte("package app\nfunc use(){ _ = Value; _ = missing }\n")},
		{Path: "a.go", Content: []byte("package app\nconst Value = 1\n")},
		{Path: "b.go", Content: []byte("package app\nconst Value = 2\n")},
		{Path: "other/other.go", Content: []byte("package other\nconst Value = 3\n")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'use'})-[r:references]->(b) RETURN r,b`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Candidate {
			t.Fatal(row)
		}
	}
	found := false
	for _, d := range g.Report().Diagnostics {
		if d.Code == "unresolved_reference" && d.Message == "missing" && d.Relation == References {
			found = true
		}
	}
	if !found {
		t.Fatal(g.Report())
	}
}

func TestModuleReferenceFacts(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.py", "def target():\n    pass\ndef use():\n    return target\n"},
		{"app.ts", "function target() {}\nfunction use() { return target; }\n"},
		{"app.js", "function target() {}\nfunction use() { return target; }\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := Build(context.Background(), "module", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'use'})-[r:references]->(:Function {name:'target'}) RETURN r`, nil)
			if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != Exact {
				t.Fatal(rows)
			}
		})
	}
}

func TestReferenceShadowing(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package app\nfunc target(){}\nfunc use(target func()){ _ = target }\n"},
		{"app.py", "def target():\n    pass\ndef use(target):\n    return target\n"},
		{"app.ts", "function target() {}\nfunction use(target: unknown) { return target; }\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := Build(context.Background(), "shadow", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'use'})-[:references]->(:Function {name:'target'}) RETURN 1`, nil)
			if len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
}

func TestReferenceImportedNameAndShadowedReceiver(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "imported", []Document{
		{Path: "app.go", Content: []byte("package app\nimport lib \"example.org/lib\"\nfunc use(){ _ = lib.Value }\nfunc shadow(lib struct{ Value int }) { _ = lib.Value }\n")},
		{Path: "lib/lib.go", Content: []byte("package lib\nconst Value = 1\n")},
	}, Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'use'})-[r:references]->(:Constant {name:'Value'}) RETURN r`, nil)
	if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != Candidate || rows[0]["r"].(Relation).Basis != "imported_name" {
		t.Fatal(rows)
	}
	rows = query(t, g, `MATCH (:Function {name:'shadow'})-[:references]->(:Constant {name:'Value'}) RETURN 1`, nil)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestReferencesRebuildAndBudget(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "batch", []Document{{Path: "use.go", Content: []byte("package app\nfunc use(){ _ = Value }\n")}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.RelationsFrom(g.Find("use.go", Function, "use")[0].ID, References)) != 0 {
		t.Fatal(g.Relations())
	}
	if err = g.AddDocuments(ctx, Document{Path: "value.go", Content: []byte("package app\nconst Value = 1\n")}); err != nil {
		t.Fatal(err)
	}
	report, err := g.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Code == "unresolved_reference" && d.Message == "Value" {
			t.Fatal(report)
		}
	}
	if len(g.RelationsFrom(g.Find("use.go", Function, "use")[0].ID, References)) != 1 {
		t.Fatal(g.Relations())
	}
	limited, err := New("budget", Options{MaxRelations: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(ctx, Document{Path: "app.go", Content: []byte("package app\nconst Value=1\nfunc use(){ _ = Value }\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(ctx); err == nil || len(limited.Nodes()) != 0 {
		t.Fatal("reference edges must respect atomic build budgets", err)
	}
}

func TestReferenceFactsExcludeBindingNamesAndText(t *testing.T) {
	g, err := New("lexical", Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), Document{Path: "app.ts", Content: []byte("function target() {}\nfunction use(target: unknown) { // target\n const text = 'target'; return target; }\n")})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, ref := range facts.References {
		if ref.Name == "target" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("declaration, parameter, comment or string counted as reference: %+v", facts.References)
	}
}
