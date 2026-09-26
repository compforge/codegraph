package codegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alitto/pond/v2"
)

var _ Identifiable = Document{}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func (g *Graph) addDocumentsSync(ctx context.Context, docs ...Document) (BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}

func TestAsyncDocumentAndSymbolWithoutWait(t *testing.T) {
	ctx := context.Background()
	g, err := New("rev", Options{})
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
	if file, ok := g.Node(doc.ID()); !ok || file.Kind != DocumentKind {
		t.Fatalf("file node = %+v, %v", file, ok)
	}
	if got, ok := g.Node(symbols[0].ID); !ok || !reflect.DeepEqual(got, symbols[0]) {
		t.Fatalf("published symbol = %+v, %v", got, ok)
	}
}

func waitBackgroundBuild(t *testing.T, g *Graph) BuildReport {
	t.Helper()
	g.asyncMu.Lock()
	work := g.latestWork
	g.asyncMu.Unlock()
	if work == nil {
		t.Fatal("no build was scheduled")
	}
	select {
	case <-work.done:
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
	g, err := New("rev", Options{})
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
	if _, ok := g.Node(doc.ID()); !ok {
		t.Fatal("wait cancellation stopped graph publication")
	}
}

func TestAddDocumentAndBatchProduceSameGraph(t *testing.T) {
	ctx := context.Background()
	docs := documents(fixture(), "main.go", "helper.go", "lib/work.go")
	batch, err := New("rev", Options{ModulePath: "example.org/demo"})
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
	single, err := New("rev", Options{ModulePath: "example.org/demo"})
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
	if !reflect.DeepEqual(batchReport, singleReport) || !reflect.DeepEqual(batch.Nodes(), single.Nodes()) || !reflect.DeepEqual(batch.Relations(), single.Relations()) {
		t.Fatal("single-document submissions changed the published graph")
	}
}

func TestAsyncBatchAdmissionIsAtomic(t *testing.T) {
	g, err := New("rev", Options{})
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
	g, err := New("rev", Options{})
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
	if node, ok := g.Node(doc.ID()); !ok || node.Kind != DocumentKind || node.Location.EndByte != len(doc.Content) {
		t.Fatal("failed parser erased the supplied file identity", node)
	}
	if len(g.Find(doc.Path, "", "")) != 0 || len(g.RelationsFrom(doc.ID())) != 0 {
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
	g, err := New("rev", Options{MaxDocuments: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := Document{Path: "first.go", Content: []byte("package p\nfunc First(){}\n")}
	second := Document{Path: "second.go", Content: []byte("package p\nfunc Second(){}\n")}
	if err := g.AddDocuments(context.Background(), first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(context.Background()); !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
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
				g, err := New("rev", Options{})
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

func TestGetDocumentRequiresSubmission(t *testing.T) {
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "main.go", Content: []byte("package p\nfunc Main(){}\n")}
	if _, err := g.GetDocument(doc.ID()); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("missing document: %v", err)
	}
	if _, err := g.Extract(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if _, err := g.GetDocument(doc.ID()); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("Extract implicitly submitted document: %v", err)
	}
	if err := g.AddDocuments(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if _, err := g.GetDocument(doc.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
