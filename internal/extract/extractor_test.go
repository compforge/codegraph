package extract

import (
	"context"
	"errors"
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
