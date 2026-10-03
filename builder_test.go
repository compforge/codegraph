package codegraph

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func newTestExtractor(t *testing.T, cache *ExtractionCache) *Extractor {
	t.Helper()
	e, err := NewExtractor(ExtractionOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func extractTestFacts(t *testing.T, e *Extractor, path, source string) Facts {
	t.Helper()
	f, err := e.Extract(context.Background(), Document{Path: path, Content: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func buildTestFacts(t *testing.T, snapshot string, opts Options, facts ...Facts) *Graph {
	t.Helper()
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(facts...); err != nil {
		t.Fatal(err)
	}
	g, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// +spec=`Facts carry all binding material without an Extractor or Graph cache`
func TestFactsOwnCompleteMaterialAndDetachedViews(t *testing.T) {
	e := newTestExtractor(t, nil)
	caller := extractTestFacts(t, e, "caller.go", "package app\nfunc Caller(){ Target() }\n")
	target := extractTestFacts(t, e, "target.go", "package app\nfunc Target(){}\n")
	caller.Path = "corrupt.go"
	caller.Declarations[0].Name = "corrupt"
	caller.Calls = nil
	before := buildTestFacts(t, "before", Options{}, caller, target)
	calls := before.RelationsFrom(before.Find("caller.go", Function, "Caller")[0].ID, Calls)
	if len(calls) != 1 {
		t.Fatalf("opaque artifact lost binding material: %+v", calls)
	}
	after := buildTestFacts(t, "after", Options{}, caller)
	if len(after.RelationsFrom(after.Find("caller.go", Function, "Caller")[0].ID, Calls)) != 0 {
		t.Fatal("deleted target leaked across snapshots")
	}
	freshCaller := extractTestFacts(t, newTestExtractor(t, nil), "caller.go", "package app\nfunc Caller(){ Target() }\n")
	fresh := buildTestFacts(t, "after", Options{}, freshCaller)
	if !reflect.DeepEqual(after.Nodes(), fresh.Nodes()) || !reflect.DeepEqual(after.Relations(), fresh.Relations()) || !reflect.DeepEqual(after.Report(), fresh.Report()) {
		t.Fatal("reused facts differ from fresh extraction")
	}
	view, err := caller.View()
	if err != nil || view.Path != "caller.go" || view.Declarations[0].Name != "Caller" {
		t.Fatalf("view aliases caller edits: %+v %v", view, err)
	}
	b, _ := NewBuilder("invalid", Options{})
	if err := b.Add(Facts{Path: "fabricated.go"}); err == nil {
		t.Fatal("partial public projection accepted as material")
	}
}

// +spec=`Builder publishes immutable results and never reparses admitted Facts`
func TestBuilderPublicationBudgetsAndLazyQueries(t *testing.T) {
	e := newTestExtractor(t, nil)
	a := extractTestFacts(t, e, "a.go", "package app\nfunc A(){}\n")
	b := extractTestFacts(t, e, "b.go", "package app\nfunc B(){}\n")
	parseObserver = func(string) { t.Error("build reparsed a Facts artifact") }
	defer func() { parseObserver = nil }()
	builder, _ := NewBuilder("snapshot", Options{MaxDocuments: 2})
	if err := builder.Add(a); err != nil {
		t.Fatal(err)
	}
	first, _, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(b); err != nil {
		t.Fatal(err)
	}
	second, _, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if builder.Result() != second || builder.session != nil {
		t.Fatal("facts-only build created document scheduling state or an extra publication")
	}
	if first.store != nil || second.store != nil {
		t.Fatal("build eagerly created a query index")
	}
	if len(first.Report().Documents) != 1 || len(second.Report().Documents) != 2 {
		t.Fatal("published result mutated")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := builder.Build(canceled); !errors.Is(err, context.Canceled) || builder.Result() != second {
		t.Fatal("canceled build changed publication")
	}
	if _, err := first.Query(canceled, "MATCH (n) RETURN n", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled query poisoned lazy storage")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			rows, err := first.Query(context.Background(), "MATCH (n:Function) RETURN n", nil)
			if err != nil || len(rows) != 1 {
				t.Errorf("concurrent lazy query: %v, %v", rows, err)
			}
		})
	}
	wg.Wait()
	limited, _ := NewBuilder("limited", Options{MaxDocuments: 1})
	if err := limited.Add(a, b); !errors.Is(err, ErrBuildBudget) {
		t.Fatal(err)
	}
	empty, _, err := limited.Build(context.Background())
	if err != nil || len(empty.Nodes()) != 0 {
		t.Fatal("failed admission left partial material")
	}
}

func TestFactsResolutionContextIsSnapshotOwned(t *testing.T) {
	e := newTestExtractor(t, nil)
	caller := extractTestFacts(t, e, "caller.py", "from dep import run\ndef caller():\n    return run()\n")
	one := extractTestFacts(t, e, "v1.py", "def run():\n    pass\n")
	two := extractTestFacts(t, e, "v2.py", "def run():\n    pass\n")
	contextFor := func(target string) ResolutionContext {
		return ResolutionContext{Imports: []ImportResolution{{Document: "caller.py", Import: caller.Imports[0], Targets: []string{target}, Confidence: NameOnly, Basis: "catalog_candidate"}}}
	}
	resolution := contextFor("v1.py")
	builder, err := NewBuilder("before", Options{ResolutionContext: resolution})
	if err != nil {
		t.Fatal(err)
	}
	resolution.Imports[0].Targets[0] = "v2.py"
	if err := builder.Add(caller, one, two); err != nil {
		t.Fatal(err)
	}
	first, _, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	check := func(g *Graph, path string) {
		t.Helper()
		calls := g.RelationsFrom(g.Find("caller.py", Function, "caller")[0].ID, Calls)
		if len(calls) != 1 {
			t.Fatalf("calls: %+v; gaps: %+v", calls, g.Report().Diagnostics)
		}
		target, _ := g.Node(calls[0].Target)
		if target.Location.Path != path || calls[0].Confidence != NameOnly {
			t.Fatalf("lost context or confidence: %+v %+v", target, calls[0])
		}
	}
	check(first, "v1.py")
	if err := builder.SetResolutionContext(contextFor("v2.py")); err != nil {
		t.Fatal(err)
	}
	second, _, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	check(second, "v2.py")
	check(first, "v1.py")
}

func TestFactsNestedGoModuleContext(t *testing.T) {
	e := newTestExtractor(t, nil)
	caller := extractTestFacts(t, e, "cmd/main.go", "package main\nimport \"example/lib\"\nfunc main(){ lib.Work() }\n")
	target := extractTestFacts(t, e, "modules/lib/work.go", "package lib\nfunc Work(){}\n")
	opts := Options{ResolutionContext: ResolutionContext{GoModules: map[string]string{".": "example/app", "modules/lib": "example/lib"}}}
	g := buildTestFacts(t, "go", opts, caller, target)
	calls := g.RelationsFrom(g.Find("cmd/main.go", Function, "main")[0].ID, Calls)
	if len(calls) != 1 {
		t.Fatalf("nested module call not bound: %+v", g.Report())
	}
	opts.ResolutionContext.GoModules["modules/lib"] = "example/renamed"
	after := buildTestFacts(t, "renamed-module", opts, caller, target)
	if len(after.RelationsFrom(after.Find("cmd/main.go", Function, "main")[0].ID, Calls)) != 0 {
		t.Fatal("module configuration reused old binding")
	}
}

func TestBuildConcurrencyOptions(t *testing.T) {
	if _, err := NewBuilder("rev", Options{BuildConcurrency: -1}); err == nil {
		t.Fatal("negative concurrency accepted")
	}
	g, err := NewBuilder("rev", Options{})
	if err != nil || g.opts.BuildConcurrency != min(runtime.GOMAXPROCS(0), 4) {
		t.Fatalf("automatic concurrency: %v, %v", g, err)
	}
}
