package codegraph

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

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
		g, err := New(snapshot, Options{ExtractionCache: cache})
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
	build := func(snapshot string, docs []Document, c *ExtractionCache) *Graph {
		t.Helper()
		g, err := New(snapshot, Options{ExtractionCache: c, BuildConcurrency: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.addDocumentsSync(context.Background(), docs...); err != nil {
			t.Fatal(err)
		}
		return g
	}
	old := build("before", before, cache)
	if len(old.RelationsFrom(old.Find("caller.go", Function, "Caller")[0].ID, Calls)) != 1 {
		t.Fatal("fixture did not bind its original call")
	}
	current := build("after", after, cache)
	if parsed["caller.go"] != 1 || parsed["target.go"] != 2 {
		t.Fatalf("parses = %v", parsed)
	}
	if got := current.RelationsFrom(current.Find("caller.go", Function, "Caller")[0].ID, Calls); len(got) != 0 {
		t.Fatalf("stale target bound from earlier snapshot: %+v", got)
	}
	fresh := build("after", after, nil)
	if !reflect.DeepEqual(current.Nodes(), fresh.Nodes()) || !reflect.DeepEqual(current.Relations(), fresh.Relations()) || !reflect.DeepEqual(current.Report(), fresh.Report()) {
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
		{"documents", 1, 1024}, {"source bytes", 10, a.size()},
	} {
		t.Run(limits.name, func(t *testing.T) {
			cache := extractionCache(t, limits.documents, limits.bytes)
			parsed := 0
			parseObserver = func(string) { parsed++ }
			defer func() { parseObserver = nil }()
			for _, snapshot := range []string{"before", "after"} {
				g, err := New(snapshot, Options{ExtractionCache: cache, BuildConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := g.addDocumentsSync(context.Background(), a, b); err != nil {
					t.Fatal(err)
				}
				if len(g.Find("a.go", Function, "A")) != 1 || len(g.Find("b.go", Function, "B")) != 1 {
					t.Fatal("full cache lost facts")
				}
			}
			if parsed != 3 {
				t.Fatalf("cache did not retain only the first document: parses=%d", parsed)
			}
			g, err := New("limited", Options{ExtractionCache: cache, MaxDocuments: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a, b); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
				t.Fatalf("cache bypassed document budget: %v", err)
			}
			g, err = New("byte-limited", Options{ExtractionCache: cache, MaxDocumentBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("cache bypassed byte budget: %v", err)
			}
			g, err = New("source-limited", Options{ExtractionCache: cache, MaxSourceBytes: a.size() - 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("cache bypassed source budget: %v", err)
			}
			g, err = New("scoped", Options{ExtractionCache: cache, Scope: []string{"other"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			report, err := g.Wait(context.Background())
			if err != nil || !hasDiagnostic(report, "out_of_scope") || len(g.Nodes()) != 0 {
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
		g, err := New("rev", Options{ExtractionCache: cache})
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
	g, err := New("cancelled", Options{ExtractionCache: cache})
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
	g, err = New("cancelled-hit", Options{ExtractionCache: cache})
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
	if _, _, err := Build(context.Background(), "warm", docs, Options{ModulePath: "example.org/demo", ExtractionCache: cache}); err != nil {
		t.Fatal(err)
	}
	// Different module contexts must not share previously bound Go imports.
	for _, modulePath := range []string{"example.org/demo", "example.org/other"} {
		fresh, _, err := Build(context.Background(), "rev", docs, Options{ModulePath: modulePath})
		if err != nil {
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		results := make(chan *Graph, 4)
		failures := make(chan error, 4)
		for i := 0; i < 4; i++ {
			workers.Go(func() {
				g, _, err := Build(context.Background(), "rev", docs, Options{ModulePath: modulePath, ExtractionCache: cache})
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
			if !reflect.DeepEqual(g.Nodes(), fresh.Nodes()) || !reflect.DeepEqual(g.Relations(), fresh.Relations()) || !reflect.DeepEqual(g.Report(), fresh.Report()) {
				t.Fatalf("shared facts changed graph in module context %s", modulePath)
			}
		}
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
