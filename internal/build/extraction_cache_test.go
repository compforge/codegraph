package build

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	extraction "github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/model"
)

func extractionCache(t *testing.T, maxDocuments int, maxSourceBytes int64) *extraction.ExtractionCache {
	t.Helper()
	cache, err := extraction.NewExtractionCache(maxDocuments, maxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestSharedExtractionCacheIdentityAndDetachedProjection(t *testing.T) {
	cache := extractionCache(t, 0, 0)
	parsed := 0
	extraction.ParseObserver = func(string) { parsed++ }
	defer func() { extraction.ParseObserver = nil }()
	extract := func(snapshot string, document extraction.Document) extraction.Facts {
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
	doc := extraction.Document{Path: "app.ts", Content: []byte("export function work() {}")}
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
	link := extraction.Document{Path: doc.Path, Gitlink: "0123456789abcdef0123456789abcdef01234567"}
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
	caller := extraction.Document{Path: "caller.go", Content: []byte("package app\nfunc Caller(){ Target() }\n")}
	before := []extraction.Document{caller, {Path: "target.go", Content: []byte("package app\nfunc Target(){}\n")}}
	after := []extraction.Document{caller, {Path: "target.go", Content: []byte("package app\nfunc Other(){}\n")}}
	parsed := map[string]int{}
	extraction.ParseObserver = func(path string) { parsed[path]++ }
	defer func() { extraction.ParseObserver = nil }()
	build := func(snapshot string, docs []extraction.Document, c *extraction.ExtractionCache) *Session {
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
	if len(old.Result().RelationsFrom(old.Result().Find("caller.go", model.Function, "Caller")[0].ID, model.Calls)) != 1 {
		t.Fatal("fixture did not bind its original call")
	}
	current := build("after", after, cache)
	if parsed["caller.go"] != 1 || parsed["target.go"] != 2 {
		t.Fatalf("parses = %v", parsed)
	}
	if got := current.Result().RelationsFrom(current.Result().Find("caller.go", model.Function, "Caller")[0].ID, model.Calls); len(got) != 0 {
		t.Fatalf("stale target bound from earlier snapshot: %+v", got)
	}
	fresh := build("after", after, nil)
	if !reflect.DeepEqual(current.Result().Nodes(), fresh.Result().Nodes()) || !reflect.DeepEqual(current.Result().Relations(), fresh.Result().Relations()) || !reflect.DeepEqual(current.Report(), fresh.Report()) {
		t.Fatal("cached graph differs from fresh snapshot construction")
	}
}

func TestSharedExtractionCacheBoundsDoNotChangeGraphBudgets(t *testing.T) {
	a := extraction.Document{Path: "a.go", Content: []byte("package app\nfunc A(){}\n")}
	b := extraction.Document{Path: "b.go", Content: []byte("package app\nfunc B(){}\n")}
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
			extraction.ParseObserver = func(string) { parsed++ }
			defer func() { extraction.ParseObserver = nil }()
			for _, snapshot := range []string{"before", "after"} {
				g, err := newTestSession(snapshot, Options{ExtractionCache: cache, BuildConcurrency: 1})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := g.addDocumentsSync(context.Background(), a, b); err != nil {
					t.Fatal(err)
				}
				if len(g.Result().Find("a.go", model.Function, "A")) != 1 || len(g.Result().Find("b.go", model.Function, "B")) != 1 {
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
			if _, err := g.Wait(context.Background()); !errors.Is(err, model.ErrBuildBudget) || len(g.Result().Nodes()) != 0 {
				t.Fatalf("cache bypassed document budget: %v", err)
			}
			g, err = newTestSession("byte-limited", Options{ExtractionCache: cache, MaxDocumentBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, model.ErrBuildBudget) {
				t.Fatalf("cache bypassed byte budget: %v", err)
			}
			g, err = newTestSession("source-limited", Options{ExtractionCache: cache, MaxSourceBytes: int64(len(a.Content)+len(a.Gitlink)) - 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.AddDocuments(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Wait(context.Background()); !errors.Is(err, model.ErrBuildBudget) {
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
	extraction.ParseObserver = func(string) { parsed++ }
	defer func() { extraction.ParseObserver = nil }()
	bad := extraction.Document{Path: "bad.go", Content: []byte("package app\nfunc broken(\n")}
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
	good := extraction.Document{Path: "good.go", Content: []byte("package app\nfunc Good(){}\n")}
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
		extraction.Document{Path: "pkg/app.py", Content: []byte("from .lib import work\ndef entry():\n    work()\n")},
		extraction.Document{Path: "pkg/lib.py", Content: []byte("def work():\n    pass\n")},
		extraction.Document{Path: "ts/app.ts", Content: []byte("import {work} from './lib'; export function entry(){work()}")},
		extraction.Document{Path: "ts/lib.ts", Content: []byte("export function work() {}")},
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
		results := make(chan *Session, 4)
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
