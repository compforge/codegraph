package codegraph

import (
	"context"
	"errors"
	"testing"
)

var _ Identifiable = Document{}

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
	task, err := g.GetDocument(doc.ID())
	if err != nil {
		t.Fatal(err)
	}
	facts, err := task.Wait()
	if err != nil || facts.Path != doc.Path || len(facts.Declarations) != 1 {
		t.Fatal(facts, err)
	}
	symbolTask, ok := g.FindAsync(doc.Path, Function, "Main")
	if !ok {
		t.Fatal("submitted symbol has no task")
	}
	symbols, err := symbolTask.Wait()
	if err != nil || len(symbols) != 1 {
		t.Fatal(symbols, err)
	}
	if _, err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if node, ok := g.Node(symbols[0].ID); !ok || node.Name != "Main" {
		t.Fatal("early symbol differs from publication", node)
	}
}
