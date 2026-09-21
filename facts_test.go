package codegraph

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestExtractReturnsDetachedFacts(t *testing.T) {
	g, err := New("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	source := fixture()
	facts, err := g.Extract(context.Background(), Document{Path: "main.go", Content: source["main.go"].Data})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Language != "go" || facts.Package != "app" || facts.Path != "main.go" {
		t.Fatalf("facts identity = %+v", facts)
	}
	var entry *FactDeclaration
	for i, d := range facts.Declarations {
		if d.Name == "Entry" {
			entry = &facts.Declarations[i]
		}
	}
	if entry == nil || entry.Kind != Function || entry.Location.Line == 0 {
		t.Fatalf("declaration = %+v", entry)
	}
	if len(entry.Markers) != 5 || entry.Markers[0].Kind != Spec {
		t.Fatalf("markers = %+v", entry.Markers)
	}
	if len(facts.Imports) != 1 || facts.Imports[0].Path != "example.org/demo/lib" || facts.Imports[0].Alias != "lib" {
		t.Fatalf("imports = %+v", facts.Imports)
	}
	if len(facts.Calls) != 3 {
		t.Fatalf("calls = %+v", facts.Calls)
	}
	if len(facts.Issues) != 0 {
		t.Fatalf("issues = %+v", facts.Issues)
	}
	if len(g.Nodes()) != 0 || len(g.Report().Files) != 0 {
		t.Fatal("Extract published graph state")
	}
}

func TestExtractThenAddParsesOnce(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := New("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "main.go", Content: source["main.go"].Data}
	if _, err := g.Extract(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddDocuments(ctx, doc, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 0 || parsed["helper.go"] != 1 {
		t.Fatalf("batch parses = %v, main.go must reuse the Extract facts", parsed)
	}
	if got := query(t, g, `MATCH (:File {path:'main.go'})-[:contains]->(f:Function {name:'Entry'}) RETURN f`, nil); len(got) != 1 {
		t.Fatalf("cached facts lost declarations: %v", got)
	}
	// The cache entry was consumed on staging; re-adding identical content must
	// hit the retained facts, not a stale cache.
	if _, err := g.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 0 {
		t.Fatalf("re-add reparsed: %v", parsed)
	}
}

func TestExtractCacheFollowsContentIdentity(t *testing.T) {
	ctx := context.Background()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Extract(ctx, Document{Path: "main.go", Content: []byte("package app\nfunc Stale(){}\n")}); err != nil {
		t.Fatal(err)
	}
	fresh := Document{Path: "main.go", Content: []byte("package app\nfunc Fresh(){}\n")}
	if _, err := g.AddDocuments(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 1 {
		t.Fatalf("changed content reused cached facts: %v", parsed)
	}
	if got := query(t, g, `MATCH (f:Function) RETURN f`, nil); len(got) != 1 || got[0]["f"].(Node).Name != "Fresh" {
		t.Fatalf("graph = %v", got)
	}
}

func TestExtractProjectsLoadedDocuments(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := New("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "helper.go", Content: source["helper.go"].Data}
	if _, err := g.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if parsed["helper.go"] != 1 {
		t.Fatalf("loaded document reparsed: %v", parsed)
	}
	if len(facts.Declarations) != 1 || facts.Declarations[0].Name != "helper" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestExtractFileOnlyDocument(t *testing.T) {
	ctx := context.Background()
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "notes.cg-unrecognized", Content: []byte("plain text notes\n")}
	facts, err := g.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Language != "" || len(facts.Declarations) != 0 {
		t.Fatalf("file-only facts = %+v", facts)
	}
	if len(facts.Issues) != 1 || facts.Issues[0].Code != "unsupported_language" {
		t.Fatalf("issues = %+v", facts.Issues)
	}
	// Cached file-only facts feed AddDocuments the same way as a direct add.
	r, err := g.AddDocuments(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Files, []string{"notes.cg-unrecognized"}) || !hasDiagnostic(r, "unsupported_language") {
		t.Fatalf("report = %+v", r)
	}
	if got := query(t, g, `MATCH (f:File {path:'notes.cg-unrecognized'}) RETURN f`, nil); len(got) != 1 {
		t.Fatalf("file node = %v", got)
	}
}

func TestExtractImportNamesAndExports(t *testing.T) {
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := g.Extract(context.Background(), Document{Path: "app.ts", Content: []byte(`
import { alpha, beta as b } from './lib';
import * as ns from './whole';
import dflt from './defaulted';
export { alpha as publicAlpha };
const local = 1;
export { local as renamed };
export { rerouted } from './other';
`)})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FactImport{}
	for _, imp := range ts.Imports {
		if _, exists := byPath[imp.Path]; !exists {
			byPath[imp.Path] = imp
		}
	}
	if got := byPath["./lib"].Names; !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("named import names = %v", got)
	}
	if got := byPath["./whole"].Names; len(got) != 0 {
		t.Fatalf("namespace import must bind the whole module: %v", got)
	}
	if got := byPath["./defaulted"].Names; len(got) != 0 {
		t.Fatalf("default import must bind the whole module: %v", got)
	}
	if got := byPath["./other"].Names; !reflect.DeepEqual(got, []string{"rerouted"}) {
		t.Fatalf("re-export names = %v", got)
	}
	want := map[string]string{"publicAlpha": "alpha", "renamed": "local"}
	if !reflect.DeepEqual(ts.Exports, want) {
		t.Fatalf("exports = %v, want %v", ts.Exports, want)
	}

	py, err := g.Extract(context.Background(), Document{Path: "worker.py", Content: []byte("from lib import work\nimport whole\nfrom lib2 import *\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(py.Imports) != 3 {
		t.Fatalf("python imports = %+v", py.Imports)
	}
	if got := py.Imports[0].Names; !reflect.DeepEqual(got, []string{"work"}) {
		t.Fatalf("from-import names = %v", got)
	}
	if len(py.Imports[1].Names) != 0 || len(py.Imports[2].Names) != 0 {
		t.Fatalf("bare and wildcard imports must bind the whole module: %+v", py.Imports)
	}
}

func TestExtractConcurrentWithBuild(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	g, err := New("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc := Document{Path: "main.go", Content: source["main.go"].Data}
			if i%2 == 1 {
				doc = Document{Path: fmt.Sprintf("extra%d.go", i), Content: []byte("package app\nfunc Extra(){}\n")}
			}
			if _, err := g.Extract(ctx, doc); err != nil {
				t.Error(err)
			}
		}()
	}
	if _, err := g.AddDocuments(ctx, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
