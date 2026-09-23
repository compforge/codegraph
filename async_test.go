package codegraph

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/alitto/pond/v2"
)

var _ Identifiable = Document{}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Flush.
func (g *Graph) addDocumentsSync(ctx context.Context, docs ...Document) (BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Flush(ctx)
}

func TestAsyncDocumentAndSymbolBeforeFlush(t *testing.T) {
	ctx := context.Background()
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){}\n")}
	if doc.ID() != FileID(doc.Path) {
		t.Fatalf("document ID = %q", doc.ID())
	}
	if err := g.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	task, ok := g.GetDocument(doc.ID())
	if !ok {
		t.Fatal("submitted document has no task")
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
	if _, ok := g.Node(doc.ID()); ok {
		t.Fatal("file node published before Flush")
	}
	if len(g.Nodes()) != 0 {
		t.Fatal("declaration published before Flush")
	}
	report, err := g.Flush(ctx)
	if err != nil || !report.Complete {
		t.Fatalf("report = %+v, %v", report, err)
	}
	if file, ok := g.Node(doc.ID()); !ok || file.Kind != File {
		t.Fatalf("file node = %+v, %v", file, ok)
	}
	if got, ok := g.Node(symbols[0].ID); !ok || !reflect.DeepEqual(got, symbols[0]) {
		t.Fatalf("published symbol = %+v, %v", got, ok)
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
	batchReport, err := batch.Flush(ctx)
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
	singleReport, err := single.Flush(ctx)
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
	if _, ok := g.GetDocument(valid.ID()); ok {
		t.Fatal("partial batch was queued")
	}
	if report, err := g.Flush(context.Background()); err != nil || len(report.Files) != 0 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	if _, err := g.AddDocument(context.Background(), invalid).Wait(); err == nil {
		t.Fatal("invalid single document accepted")
	}
}

func TestAsyncParseFailureIsNotRetriedOnFlush(t *testing.T) {
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
	task, ok := g.GetDocument(doc.ID())
	if !ok {
		t.Fatal("missing failed-document task")
	}
	if _, err := task.Wait(); err == nil {
		t.Fatal("invalid source parsed successfully")
	}
	report, err := g.Flush(context.Background())
	if err != nil || !hasDiagnostic(report, "parse_error") || count != 1 {
		t.Fatalf("report = %+v, count=%d, err=%v", report, count, err)
	}
	if _, ok := g.Node(doc.ID()); ok {
		t.Fatal("failed file published")
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
				if _, err := g.Flush(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
