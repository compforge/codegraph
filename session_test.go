package codegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alitto/pond/v2"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var _ Identifiable = Document{}

func TestAsyncDocumentAndSymbolWithoutWait(t *testing.T) {
	ctx := context.Background()
	g, err := newTestSession("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){}\n")}
	if doc.ID() != DocumentID(doc.Path) {
		t.Fatalf("document ID = %q", doc.ID())
	}
	if err := g.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	task, err := g.GetDocument(doc.ID())
	if err != nil {
		t.Fatal(err)
	}
	facts, err := task.Wait()
	if err != nil || facts.Path != doc.Path || len(facts.Declarations) != 1 {
		t.Fatalf("facts = %+v, %v", facts, err)
	}
	symbolTask, ok := g.FindAsync(doc.Path, Function, "Entry")
	if !ok {
		t.Fatal("submitted symbol has no task")
	}
	symbols, err := symbolTask.Wait()
	if err != nil || len(symbols) != 1 || symbols[0].Name != "Entry" {
		t.Fatalf("symbols = %+v, %v", symbols, err)
	}
	// Observe the build barrier directly: publication must finish even if the
	// caller never invokes Wait.
	background := waitBackgroundBuild(t, g)
	if len(background.Diagnostics) != 0 {
		t.Fatalf("background report = %+v", background)
	}
	report, err := g.Wait(ctx)
	if !reflect.DeepEqual(report, background) {
		t.Fatalf("Wait changed the built report: before=%+v after=%+v", background, report)
	}
	if err != nil || len(report.Diagnostics) != 0 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	if file, ok := g.Result().Node(doc.ID()); !ok || file.Kind != DocumentKind {
		t.Fatalf("file node = %+v, %v", file, ok)
	}
	if got, ok := g.Result().Node(symbols[0].ID); !ok || !reflect.DeepEqual(got, symbols[0]) {
		t.Fatalf("published symbol = %+v, %v", got, ok)
	}
}

func waitBackgroundBuild(t *testing.T, g *session) BuildReport {
	t.Helper()
	g.asyncMu.Lock()
	work := g.latestWork
	g.asyncMu.Unlock()
	var done <-chan struct{}
	if work != nil {
		done = work.done
	}
	if done == nil {
		t.Fatal("no build was scheduled")
	}
	select {
	case <-done:
		if work.err != nil {
			t.Fatal(work.err)
		}
		return cloneReport(work.report)
	case <-time.After(10 * time.Second):
		t.Fatal("background graph build did not complete")
		return BuildReport{}
	}
}

func TestWaitCancellationDoesNotCancelBuild(t *testing.T) {
	g, err := newTestSession("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	parseObserver = func(string) { close(started); <-release }
	defer func() { parseObserver = nil }()
	doc := Document{Path: "wait.go", Content: []byte("package p\nfunc Wait(){}\n")}
	if err := g.AddDocuments(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("extraction did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
	close(release)
	if report := waitBackgroundBuild(t, g); len(report.Documents) != 1 {
		t.Fatalf("background build after canceled wait = %+v", report)
	}
	if _, ok := g.Result().Node(doc.ID()); !ok {
		t.Fatal("wait cancellation stopped graph publication")
	}
}

func TestAddDocumentAndBatchProduceSameGraph(t *testing.T) {
	ctx := context.Background()
	docs := documents(fixture(), "main.go", "helper.go", "lib/work.go")
	batch, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.AddDocuments(ctx, docs...); err != nil {
		t.Fatal(err)
	}
	batchReport, err := batch.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	single, err := newTestSession("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := make([]pond.ResultTask[Facts], 0, len(docs))
	for _, doc := range docs {
		tasks = append(tasks, single.AddDocument(ctx, doc))
	}
	for i, task := range tasks {
		if facts, err := task.Wait(); err != nil || facts.Path != docs[i].Path {
			t.Fatalf("%s: %+v, %v", docs[i].Path, facts, err)
		}
	}
	singleReport, err := single.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batchReport, singleReport) || !reflect.DeepEqual(batch.Result().Nodes(), single.Result().Nodes()) || !reflect.DeepEqual(batch.Result().Relations(), single.Result().Relations()) {
		t.Fatal("single-document submissions changed the published graph")
	}
}

func TestAsyncBatchAdmissionIsAtomic(t *testing.T) {
	g, err := newTestSession("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	valid := Document{Path: "main.go", Content: []byte("package p\n")}
	invalid := Document{Path: "../outside.go", Content: valid.Content}
	if err := g.AddDocuments(context.Background(), valid, invalid); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if _, err := g.GetDocument(valid.ID()); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("partial batch was queued: %v", err)
	}
	if report, err := g.Wait(context.Background()); err != nil || len(report.Documents) != 0 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	if _, err := g.AddDocument(context.Background(), invalid).Wait(); err == nil {
		t.Fatal("invalid single document accepted")
	}
}

func TestAsyncParseFailureIsNotRetriedByWait(t *testing.T) {
	g, err := newTestSession("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	parseObserver = func(string) { count++ }
	defer func() { parseObserver = nil }()
	doc := Document{Path: "bad.go", Content: []byte("package p\nfunc (")}
	if err := g.AddDocuments(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	task, err := g.GetDocument(doc.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := task.Wait(); err == nil {
		t.Fatal("invalid source parsed successfully")
	}
	report, err := g.Wait(context.Background())
	retained, getErr := g.GetDocument(doc.ID())
	if getErr != nil || retained != task {
		t.Fatalf("submitted failed document lost its task: %v", getErr)
	}
	if err != nil || !hasDiagnostic(report, "parse_error") || count != 1 {
		t.Fatalf("report = %+v, count=%d, err=%v", report, count, err)
	}
	if node, ok := g.Result().Node(doc.ID()); !ok || node.Kind != DocumentKind || node.Location.EndByte != len(doc.Content) {
		t.Fatal("failed parser erased the supplied file identity", node)
	}
	if len(g.Result().Find(doc.Path, "", "")) != 0 || len(g.Result().RelationsFrom(doc.ID())) != 0 {
		t.Fatal("failed parser invented declarations or relations")
	}
	if _, err := g.AddDocument(context.Background(), doc).Wait(); err == nil {
		t.Fatal("failed document was not retried on a new submission")
	}
	if _, err := g.Wait(context.Background()); err != nil || count != 2 {
		t.Fatalf("retry report: count=%d, err=%v", count, err)
	}
}

func TestBackgroundBuildFailureCanBeRetried(t *testing.T) {
	g, err := newTestSession("rev", Options{MaxDocuments: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := Document{Path: "first.go", Content: []byte("package p\nfunc First(){}\n")}
	second := Document{Path: "second.go", Content: []byte("package p\nfunc Second(){}\n")}
	if err := g.AddDocuments(context.Background(), first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) || len(g.Result().Nodes()) != 0 {
		t.Fatalf("failed background batch published: %v", err)
	}
	if err := g.AddDocuments(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if report, err := g.Wait(context.Background()); err != nil || len(report.Documents) != 1 {
		t.Fatalf("retry report = %+v, %v", report, err)
	}
}

// BenchmarkDocumentSubmission compares only the submission shape; both cases
// parse through the same bounded pool and publish once per complete workset.
func BenchmarkDocumentSubmission(b *testing.B) {
	docs := make([]Document, 96)
	for i := range docs {
		docs[i] = Document{Path: fmt.Sprintf("f%d.go", i), Content: []byte("package p\nfunc Entry(){}\n")}
	}
	for _, batch := range []bool{true, false} {
		name := "single"
		if batch {
			name = "batch"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				g, err := newTestSession("rev", Options{})
				if err != nil {
					b.Fatal(err)
				}
				if batch {
					err = g.AddDocuments(context.Background(), docs...)
				} else {
					for _, doc := range docs {
						g.AddDocument(context.Background(), doc)
					}
				}
				if err != nil {
					b.Fatal(err)
				}
				if _, err := g.Wait(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestParallelDocumentsEquivalent(t *testing.T) {
	docs := documents(fixture(), "main.go", "helper.go", "lib/work.go", "entry_test.go")
	docs = append(docs,
		Document{Path: "worker.py", Content: []byte("def work():\n    pass\ndef entry():\n    work()\n")},
		Document{Path: "app.ts", Content: []byte("function work() {}\nfunction entry() { work(); }\n")},
		Document{Path: "bad.go", Content: []byte("package p\nfunc (")},
		docs[0],
	)
	var baseline *session
	for _, concurrency := range []int{1, 2, 4, 8} {
		g, _, err := buildTestSession(context.Background(), "rev", docs, Options{BuildConcurrency: concurrency, ModulePath: "example.org/demo"})
		if err != nil {
			t.Fatal(err)
		}
		if baseline == nil {
			baseline = g
		} else if !reflect.DeepEqual(g.Result().Nodes(), baseline.Result().Nodes()) || !reflect.DeepEqual(g.Result().Relations(), baseline.Result().Relations()) || !reflect.DeepEqual(g.Report(), baseline.Report()) {
			t.Fatalf("concurrency %d changed facts or diagnostics", concurrency)
		}
	}
}

func TestParallelFailedParseReleasesBudget(t *testing.T) {
	good := []byte("package p\nfunc Work() {}\n")
	docs := []Document{{Path: "bad.go", Content: []byte("package p\nfunc (")},
		{Path: "a.go", Content: good}, {Path: "b.go", Content: good}}
	for _, concurrency := range []int{1, 4} {
		g, report, err := buildTestSession(context.Background(), "rev", docs, Options{
			BuildConcurrency: concurrency, MaxDocuments: 2, MaxSourceBytes: int64(2 * len(good)),
		})
		if err != nil || len(report.Documents) != 2 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "parse_error" {
			t.Fatalf("concurrency %d: %+v, %v", concurrency, report, err)
		}
		nodes, relations := g.Result().Nodes(), g.Result().Relations()
		if _, err := g.addDocumentsSync(context.Background(), Document{Path: "c.go", Content: good}); !errors.Is(err, ErrBuildBudget) {
			t.Fatalf("expected exhausted budget: %v", err)
		}
		if !reflect.DeepEqual(report, g.Report()) || !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) {
			t.Fatal("budget failure published staged facts")
		}
	}
}

func TestParallelExtractionBoundAndAtomicPublication(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{}, 10)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var active, peak atomic.Int32
			entry := *grammars.DetectLanguageByName("python")
			loader := entry.Language
			ext := fmt.Sprintf(".cgparallel%v", canceled)
			entry.Name, entry.Extensions = "codegraph-parallel-"+ext, []string{ext}
			entry.Language = func() *gts.Language {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				started <- struct{}{}
				<-release
				return loader()
			}
			grammars.Register(entry)
			g, _, err := buildTestSession(ctx, "rev", []Document{{Path: "seed.go", Content: []byte("package p\nfunc Seed() {}")}}, Options{BuildConcurrency: 3})
			if err != nil {
				t.Fatal(err)
			}
			nodes, report := g.Result().Nodes(), g.Report()
			var docs []Document
			for i := 0; i < 7; i++ {
				docs = append(docs, Document{Path: fmt.Sprintf("f%d%s", i, ext), Content: []byte("def work():\n    pass\n")})
			}
			done := make(chan error, 1)
			go func() { _, err := g.addDocumentsSync(ctx, docs...); done <- err }()
			for i := 0; i < 3; i++ {
				select {
				case <-started:
				case <-time.After(10 * time.Second):
					t.Fatal("document workers did not run concurrently")
				}
			}
			if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(report, g.Report()) {
				t.Fatal("in-flight extraction changed published graph")
			}
			if rows := querySession(t, g, "MATCH (n:Function {name:'Seed'}) RETURN n", nil); len(rows) != 1 {
				t.Fatal("old graph unavailable during build")
			}
			if canceled {
				cancel()
			}
			unblock()
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("workers did not finish")
			}
			if canceled {
				// Wait cancellation is independent of background worker lifetime.
				g.asyncMu.Lock()
				done := g.latestWork.done
				g.asyncMu.Unlock()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("canceled build did not stop its workers")
				}
			}
			if peak.Load() != 3 || active.Load() != 0 {
				t.Fatalf("worker bound/lifetime: peak=%d active=%d", peak.Load(), active.Load())
			}
			if canceled {
				if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(report, g.Report()) {
					t.Fatalf("canceled batch changed graph: %v", err)
				}
			} else if err != nil || len(g.Report().Documents) != 8 {
				t.Fatalf("batch did not publish: %+v, %v", g.Report(), err)
			}
		})
	}
}

// BenchmarkDocumentBuild compares the same mixed-language workset across worker
// counts, including resolution and graph storage rather than parser time alone.
func BenchmarkDocumentBuild(b *testing.B) {
	var docs []Document
	for i := 0; i < 48; i++ {
		docs = append(docs,
			Document{Path: fmt.Sprintf("p%d/app.go", i), Content: []byte("package p\nfunc Work() {}\nfunc Entry() { Work() }\n")},
			Document{Path: fmt.Sprintf("p%d/app.py", i), Content: []byte("def work():\n    pass\ndef entry():\n    work()\n")},
			Document{Path: fmt.Sprintf("p%d/app.ts", i), Content: []byte("function work() {}\nfunction entry() { work(); }\n")},
		)
	}
	// Grammar loading is process-wide and lazy; warm every language before
	// comparing worker counts so the first configuration does not pay alone.
	if _, _, err := buildTestSession(context.Background(), "warmup", docs, Options{BuildConcurrency: 1}); err != nil {
		b.Fatal(err)
	}
	for _, concurrency := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", concurrency), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, report, err := buildTestSession(context.Background(), "bench", docs, Options{BuildConcurrency: concurrency})
				if err != nil || len(report.Diagnostics) != 0 {
					b.Fatalf("%+v, %v", report, err)
				}
			}
		})
	}
}
