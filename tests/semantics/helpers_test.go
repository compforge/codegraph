package semantics_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/compforge/codegraph"
)

func query(t *testing.T, g *codegraph.Graph, q string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.Query(context.Background(), q, params)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func addDocumentsSync(g *codegraph.Builder, ctx context.Context, docs ...codegraph.Document) (codegraph.BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}

func documents(source fstest.MapFS, paths ...string) []codegraph.Document {
	out := make([]codegraph.Document, 0, len(paths))
	for _, path := range paths {
		out = append(out, codegraph.Document{Path: path, Content: source[path].Data})
	}
	return out
}

func buildTestBuilder(ctx context.Context, snapshot string, docs []codegraph.Document, opts codegraph.Options) (*codegraph.Builder, codegraph.BuildReport, error) {
	b, err := codegraph.NewBuilder(snapshot, opts)
	if err != nil {
		return nil, codegraph.BuildReport{}, err
	}
	if err := b.AddDocuments(ctx, docs...); err != nil {
		return b, b.Report(), err
	}
	report, err := b.Wait(ctx)
	return b, report, err
}
