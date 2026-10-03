package codegraph

import (
	"context"
	"slices"
	"testing"
	"testing/fstest"
)

func hasDiagnostic(r BuildReport, code string) bool {
	return slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == code })
}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func (g *Builder) addDocumentsSync(ctx context.Context, docs ...Document) (BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"main.go":       {Data: []byte("package app\nimport lib \"example.org/demo/lib\"\n// +spec=`review keeps source evidence`\n// +case:id=call,expect=`two calls`\n// +rule=`do not merge call sites`\n// +link=docs/review.md\n// +doc=`entrypoint documentation`\nfunc Entry(){ lib.Work(); lib.Work(); helper() }\n")},
		"helper.go":     {Data: []byte("package app\nfunc helper(){ helper() }\n")},
		"lib/work.go":   {Data: []byte("package worker\nfunc Work(){ End() }\nfunc End(){}\n")},
		"entry_test.go": {Data: []byte("package app\n// +case=`entry regression`\nfunc TestEntry(){ Entry() }\n")},
	}
}

func documents(source fstest.MapFS, paths ...string) []Document {
	out := make([]Document, 0, len(paths))
	for _, path := range paths {
		out = append(out, Document{Path: path, Content: source[path].Data})
	}
	return out
}

func built(t *testing.T, opts Options) *Graph {
	t.Helper()
	opts.ModulePath = "example.org/demo"
	g, r, err := Build(context.Background(), "rev-A", documents(fixture(), "main.go", "helper.go", "lib/work.go", "entry_test.go"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("partial: %+v", r)
	}
	return g
}

func query(t *testing.T, g *Graph, q string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.Query(context.Background(), q, params)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func querySession(t *testing.T, g *session, q string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.Result().Query(context.Background(), q, params)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func (g *session) addDocumentsSync(ctx context.Context, docs ...Document) (BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}

func newTestSession(snapshot string, opts Options) (*session, error) {
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, err
	}
	return newSession(b)
}

func buildTestSession(ctx context.Context, snapshot string, docs []Document, opts Options) (*session, BuildReport, error) {
	s, err := newTestSession(snapshot, opts)
	if err != nil {
		return nil, BuildReport{}, err
	}
	if err := s.AddDocuments(ctx, docs...); err != nil {
		return nil, s.Report(), err
	}
	r, err := s.Wait(ctx)
	return s, r, err
}

func buildTestBuilder(ctx context.Context, snapshot string, docs []Document, opts Options) (*Builder, BuildReport, error) {
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, BuildReport{}, err
	}
	if err := b.AddDocuments(ctx, docs...); err != nil {
		return b, b.Report(), err
	}
	report, err := b.Wait(ctx)
	return b, report, err
}

func builtBuilder(t *testing.T, opts Options) *Builder {
	t.Helper()
	opts.ModulePath = "example.org/demo"
	b, _, err := buildTestBuilder(context.Background(), "rev-A", documents(fixture(), "main.go", "helper.go", "lib/work.go", "entry_test.go"), opts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
