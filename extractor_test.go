package codegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestExtractorTaskOwnershipAndCancellation(t *testing.T) {
	e, err := NewExtractor(ExtractionOptions{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "a.ts", Content: []byte("export function run() {}")}
	task := e.Submit(context.Background(), doc)
	for i := range doc.Content {
		doc.Content[i] = 'x'
	}
	first, err := task.Wait()
	if err != nil {
		t.Fatal(err)
	}
	first.Declarations[0].Name = "corrupt"
	second, err := task.Wait()
	if err != nil || second.Declarations[0].Name != "run" {
		t.Fatal("task projections alias")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Submit(ctx, doc).Wait(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := e.Extract(ctx, doc); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNewExtractionCacheRejectsNegativeLimits(t *testing.T) {
	for _, limits := range []struct {
		documents int
		bytes     int64
	}{{-1, 0}, {0, -1}} {
		if _, err := NewExtractionCache(limits.documents, limits.bytes); err == nil {
			t.Fatal("negative cache limit accepted")
		}
	}
}

func TestExtractReturnsDetachedFacts(t *testing.T) {
	g, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
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
	if len(g.Result().Nodes()) != 0 || len(g.Report().Documents) != 0 {
		t.Fatal("Extract published graph state")
	}
}

func TestExtractThenAddParsesOnce(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "main.go", Content: source["main.go"].Data}
	if _, err := g.Extract(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := g.addDocumentsSync(ctx, doc, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 1 || parsed["helper.go"] != 1 {
		t.Fatalf("batch parses = %v, main.go must reuse the Extract facts", parsed)
	}
	if got := querySession(t, g, `MATCH (:Document {path:'main.go'})-[:declares]->(f:Function {name:'Entry'}) RETURN f`, nil); len(got) != 1 {
		t.Fatalf("cached facts lost declarations: %v", got)
	}
	// The cache entry was consumed on staging; re-adding identical content must
	// hit the retained facts, not a stale cache.
	if _, err := g.addDocumentsSync(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 1 {
		t.Fatalf("re-add reparsed: %v", parsed)
	}
}

func TestExtractCacheFollowsContentIdentity(t *testing.T) {
	ctx := context.Background()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := newTestSession("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Extract(ctx, Document{Path: "main.go", Content: []byte("package app\nfunc Stale(){}\n")}); err != nil {
		t.Fatal(err)
	}
	fresh := Document{Path: "main.go", Content: []byte("package app\nfunc Fresh(){}\n")}
	if _, err := g.addDocumentsSync(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if parsed["main.go"] != 2 {
		t.Fatalf("changed content reused cached facts: %v", parsed)
	}
	if got := querySession(t, g, `MATCH (f:Function) RETURN f`, nil); len(got) != 1 || got[0]["f"].(Node).Name != "Fresh" {
		t.Fatalf("graph = %v", got)
	}
}

func TestExtractProjectsLoadedDocuments(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	g, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "helper.go", Content: source["helper.go"].Data}
	if _, err := g.addDocumentsSync(ctx, doc); err != nil {
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

func TestExtractDocumentOnlyDocument(t *testing.T) {
	ctx := context.Background()
	g, err := newTestSession("rev", Options{})
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
	r, err := g.addDocumentsSync(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Documents, []string{"notes.cg-unrecognized"}) || !hasDiagnostic(r, "unsupported_language") {
		t.Fatalf("report = %+v", r)
	}
	if got := querySession(t, g, `MATCH (f:Document {path:'notes.cg-unrecognized'}) RETURN f`, nil); len(got) != 1 {
		t.Fatalf("file node = %v", got)
	}
}

func TestExtractImportNamesAndExports(t *testing.T) {
	g, err := newTestSession("rev", Options{})
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
	g, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
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
	if _, err := g.addDocumentsSync(ctx, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func extractionCache(t *testing.T, maxDocuments int, maxSourceBytes int64) *ExtractionCache {
	t.Helper()
	cache, err := NewExtractionCache(maxDocuments, maxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestSharedExtractionCacheIdentityAndDetachedProjection(t *testing.T) {
	cache := extractionCache(t, 0, 0)
	parsed := 0
	parseObserver = func(string) { parsed++ }
	defer func() { parseObserver = nil }()
	extract := func(snapshot string, document Document) Facts {
		t.Helper()
		g, err := newTestSession(snapshot, Options{ExtractionCache: cache})
		if err != nil {
			t.Fatal(err)
		}
		facts, err := g.Extract(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		return facts
	}
	doc := Document{Path: "app.ts", Content: []byte("export function work() {}")}
	first := extract("before", doc)
	first.Declarations[0].Name = "corrupted"
	second := extract("after", doc)
	if parsed != 1 || second.Declarations[0].Name != "work" {
		t.Fatalf("unchanged facts were reparsed or aliased: parses=%d facts=%+v", parsed, second)
	}
	doc.Content = []byte("export function changed() {}")
	if got := extract("changed", doc); parsed != 2 || got.Declarations[0].Name != "changed" {
		t.Fatalf("changed contents reused old facts: parses=%d facts=%+v", parsed, got)
	}
	doc.Path = "other/app.ts"
	if got := extract("renamed", doc); parsed != 3 || got.Path != doc.Path {
		t.Fatalf("changed path reused old locations: parses=%d facts=%+v", parsed, got)
	}
	link := Document{Path: doc.Path, Gitlink: "0123456789abcdef0123456789abcdef01234567"}
	if got := extract("gitlink", link); got.Gitlink != link.Gitlink || len(got.Declarations) != 0 {
		t.Fatalf("material type reused source facts: %+v", got)
	}
	link.Gitlink = "1123456789abcdef0123456789abcdef01234567"
	if got := extract("gitlink-new", link); got.Gitlink != link.Gitlink {
		t.Fatal(got)
	}
}

func TestSharedExtractionCacheRebindsEachSnapshot(t *testing.T) {
	cache := extractionCache(t, 0, 0)
	caller := Document{Path: "caller.go", Content: []byte("package app\nfunc Caller(){ Target() }\n")}
	before := []Document{caller, {Path: "target.go", Content: []byte("package app\nfunc Target(){}\n")}}
	after := []Document{caller, {Path: "target.go", Content: []byte("package app\nfunc Other(){}\n")}}
	parsed := map[string]int{}
	parseObserver = func(path string) { parsed[path]++ }
	defer func() { parseObserver = nil }()
	build := func(snapshot string, docs []Document, c *ExtractionCache) *session {
		t.Helper()
		g, err := newTestSession(snapshot, Options{ExtractionCache: c, BuildConcurrency: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.addDocumentsSync(context.Background(), docs...); err != nil {
			t.Fatal(err)
		}
		return g
	}
	old := build("before", before, cache)
	if len(old.Result().RelationsFrom(old.Result().Find("caller.go", Function, "Caller")[0].ID, Calls)) != 1 {
		t.Fatal("fixture did not bind its original call")
	}
	current := build("after", after, cache)
	if parsed["caller.go"] != 1 || parsed["target.go"] != 2 {
		t.Fatalf("parses = %v", parsed)
	}
	if got := current.Result().RelationsFrom(current.Result().Find("caller.go", Function, "Caller")[0].ID, Calls); len(got) != 0 {
		t.Fatalf("stale target bound from earlier snapshot: %+v", got)
	}
	fresh := build("after", after, nil)
	if !reflect.DeepEqual(current.Result().Nodes(), fresh.Result().Nodes()) || !reflect.DeepEqual(current.Result().Relations(), fresh.Result().Relations()) || !reflect.DeepEqual(current.Report(), fresh.Report()) {
		t.Fatal("cached graph differs from fresh snapshot construction")
	}
}

func TestSharedExtractionCacheBoundsDoNotChangeGraphBudgets(t *testing.T) {
	a := Document{Path: "a.go", Content: []byte("package app\nfunc A(){}\n")}
	b := Document{Path: "b.go", Content: []byte("package app\nfunc B(){}\n")}
	for _, limits := range []struct {
		name      string
		documents int
		bytes     int64
	}{
		{"documents", 1, 1024}, {"source bytes", 10, int64(len(a.Content) + len(a.Gitlink))},
	} {
		t.Run(limits.name, func(t *testing.T) {
			cache := extractionCache(t, limits.documents, limits.bytes)
			parsed := 0
			parseObserver = func(string) { parsed++ }
			defer func() { parseObserver = nil }()
			for _, snapshot := range []string{"before", "after"} {
				g, err := newTestSession(snapshot, Options{ExtractionCache: cache, BuildConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := g.addDocumentsSync(context.Background(), a, b); err != nil {
					t.Fatal(err)
				}
				if len(g.Result().Find("a.go", Function, "A")) != 1 || len(g.Result().Find("b.go", Function, "B")) != 1 {
					t.Fatal("full cache lost facts")
				}
			}
			if parsed != 3 {
				t.Fatalf("cache did not retain only the first document: parses=%d", parsed)
			}
			g, err := newTestSession("limited", Options{ExtractionCache: cache, MaxDocuments: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a, b); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) || len(g.Result().Nodes()) != 0 {
				t.Fatalf("cache bypassed document budget: %v", err)
			}
			g, err = newTestSession("byte-limited", Options{ExtractionCache: cache, MaxDocumentBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("cache bypassed byte budget: %v", err)
			}
			g, err = newTestSession("source-limited", Options{ExtractionCache: cache, MaxSourceBytes: int64(len(a.Content)+len(a.Gitlink)) - 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("cache bypassed source budget: %v", err)
			}
			g, err = newTestSession("scoped", Options{ExtractionCache: cache, Scope: []string{"other"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			report, err := g.Wait(context.Background())
			if err != nil || !hasDiagnostic(report, "out_of_scope") || len(g.Result().Nodes()) != 0 {
				t.Fatalf("cache bypassed scope: report=%+v err=%v", report, err)
			}
		})
	}
}

func TestSharedExtractionCacheDoesNotRetainFailedOrCancelledExtraction(t *testing.T) {
	cache := extractionCache(t, 0, 0)
	parsed := 0
	parseObserver = func(string) { parsed++ }
	defer func() { parseObserver = nil }()
	bad := Document{Path: "bad.go", Content: []byte("package app\nfunc broken(\n")}
	for i := 0; i < 2; i++ {
		g, err := newTestSession("rev", Options{ExtractionCache: cache})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Extract(context.Background(), bad); err == nil {
			t.Fatal("expected parse failure")
		}
	}
	if parsed != 2 {
		t.Fatalf("parse failure retained: parses=%d", parsed)
	}
	g, err := newTestSession("cancelled", Options{ExtractionCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	good := Document{Path: "good.go", Content: []byte("package app\nfunc Good(){}\n")}
	if _, err := g.Extract(ctx, good); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := g.Extract(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	if parsed != 3 {
		t.Fatalf("cancellation poisoned retry: parses=%d", parsed)
	}
	// A new Graph must also honor cancellation when shared facts already exist.
	g, err = newTestSession("cancelled-hit", Options{ExtractionCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Extract(ctx, good); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSharedExtractionCacheConcurrentSnapshotsMatchFreshGraphs(t *testing.T) {
	cache := extractionCache(t, 0, 0)
	docs := documents(fixture(), "main.go", "helper.go", "lib/work.go", "entry_test.go")
	docs = append(docs,
		Document{Path: "pkg/app.py", Content: []byte("from .lib import work\ndef entry():\n    work()\n")},
		Document{Path: "pkg/lib.py", Content: []byte("def work():\n    pass\n")},
		Document{Path: "ts/app.ts", Content: []byte("import {work} from './lib'; export function entry(){work()}")},
		Document{Path: "ts/lib.ts", Content: []byte("export function work() {}")},
	)
	if _, _, err := buildTestSession(context.Background(), "warm", docs, Options{ModulePath: "example.org/demo", ExtractionCache: cache}); err != nil {
		t.Fatal(err)
	}
	// Different module contexts must not share previously bound Go imports.
	for _, modulePath := range []string{"example.org/demo", "example.org/other"} {
		fresh, _, err := buildTestSession(context.Background(), "rev", docs, Options{ModulePath: modulePath})
		if err != nil {
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		results := make(chan *session, 4)
		failures := make(chan error, 4)
		for i := 0; i < 4; i++ {
			workers.Go(func() {
				g, _, err := buildTestSession(context.Background(), "rev", docs, Options{ModulePath: modulePath, ExtractionCache: cache})
				if err != nil {
					failures <- err
					return
				}
				results <- g
			})
		}
		workers.Wait()
		close(results)
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		for g := range results {
			if !reflect.DeepEqual(g.Result().Nodes(), fresh.Result().Nodes()) || !reflect.DeepEqual(g.Result().Relations(), fresh.Result().Relations()) || !reflect.DeepEqual(g.Report(), fresh.Report()) {
				t.Fatalf("shared facts changed graph in module context %s", modulePath)
			}
		}
	}
}
