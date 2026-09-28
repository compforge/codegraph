package codegraph

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// +case=`ECMAScript modules own declarations while source documents retain provenance; namespace references target modules`
func TestNamespaceECMAScriptModules(t *testing.T) {
	ctx := context.Background()
	docs := []Document{
		{Path: "src/app.ts", Content: []byte("import * as api from './lib'; export function entry(){ return api; }")},
		{Path: "src/lib.js", Content: []byte("export class Box { run() {} } export function work() {}")},
		{Path: "other/lib.js", Content: []byte("export function work() {}")},
		{Path: "src/empty.tsx", Content: []byte("")},
	}
	g, _, err := Build(ctx, "modules", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if modules := query(t, g, `MATCH (m:Module) RETURN m`, nil); len(modules) != 4 {
		t.Fatalf("file modules: %v", modules)
	}
	rows := query(t, g, `MATCH (:Document {path:'src/lib.js'})-[:declares]->(m:Module)-[:contains]->(:Class {name:'Box'})-[:contains]->(:Method {name:'run'}) RETURN m`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	module := rows[0]["m"].(Node)
	if module.Location != nil || module.Name != "lib" || module.QualifiedName != "src/lib" {
		t.Fatal(module)
	}
	for _, q := range []string{
		`MATCH (:Document {path:'src/app.ts'})-[:imports]->(m:Module {id:$id}) RETURN m`,
		`MATCH (:Function {name:'entry'})-[:references]->(m:Module {id:$id}) RETURN m`,
	} {
		if rows := query(t, g, q, map[string]any{"id": module.ID}); len(rows) != 1 {
			t.Fatalf("lost module target: %v", rows)
		}
	}
	if rows := query(t, g, `MATCH (:Document)-[:contains]->() RETURN 1`, nil); len(rows) != 0 {
		t.Fatal("document membership", rows)
	}
	if rows := query(t, g, `MATCH ()-[:references]->(:Document) RETURN 1`, nil); len(rows) != 0 {
		t.Fatal("namespace reference fell back to source material", rows)
	}
	if rows := query(t, g, `MATCH (m:Module {qualifiedName:'src/empty'})-[:contains]->() RETURN m`, nil); len(rows) != 0 {
		t.Fatal("empty module has members", rows)
	}
	for _, lang := range []string{"javascript", "typescript", "tsx"} {
		if cap := Capabilities(lang); len(cap) != 1 || !reflect.DeepEqual(cap[0].Organizations, []NodeKind{Module}) {
			t.Fatal(cap)
		}
	}
	// Rebuilding from a later batch must not change source-module identities.
	later, _, err := Build(ctx, "modules", docs[1:2], Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = later.AddDocuments(ctx, docs[0], docs[2], docs[3]); err != nil {
		t.Fatal(err)
	}
	if _, err = later.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Nodes(), later.Nodes()) || !reflect.DeepEqual(g.Relations(), later.Relations()) {
		t.Fatal("batch order changed the organization graph")
	}
}

func TestNamespaceECMAScriptBudgetRollback(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "empty.ts", Content: []byte("")}
	if _, _, err := Build(ctx, "budget", []Document{doc}, Options{MaxNodes: 1}); !errors.Is(err, ErrBuildBudget) {
		t.Fatalf("module not included in node budget: %v", err)
	}
	g, _, err := Build(ctx, "budget", []Document{doc}, Options{MaxNodes: 2})
	if err != nil {
		t.Fatal(err)
	}
	before := g.Nodes()
	if err = g.AddDocuments(ctx, Document{Path: "other.ts", Content: []byte("")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); !errors.Is(err, ErrBuildBudget) {
		t.Fatalf("want budget error: %v", err)
	}
	if !reflect.DeepEqual(before, g.Nodes()) {
		t.Fatal("failed batch published partial namespace graph")
	}
}
