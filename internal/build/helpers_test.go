package build

import (
	"context"
	"slices"
	"testing"
	"testing/fstest"

	extraction "github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/model"
)

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"main.go":       {Data: []byte("package app\nimport lib \"example.org/demo/lib\"\n// +spec=`review keeps source evidence`\n// +case:id=call,expect=`two calls`\n// +rule=`do not merge call sites`\n// +link=docs/review.md\n// +doc=`entrypoint documentation`\nfunc Entry(){ lib.Work(); lib.Work(); helper() }\n")},
		"helper.go":     {Data: []byte("package app\nfunc helper(){ helper() }\n")},
		"lib/work.go":   {Data: []byte("package worker\nfunc Work(){ End() }\nfunc End(){}\n")},
		"entry_test.go": {Data: []byte("package app\n// +case=`entry regression`\nfunc TestEntry(){ Entry() }\n")},
	}
}

func documents(source fstest.MapFS, paths ...string) []extraction.Document {
	out := make([]extraction.Document, 0, len(paths))
	for _, path := range paths {
		out = append(out, extraction.Document{Path: path, Content: source[path].Data})
	}
	return out
}

func query(t *testing.T, g *Session, q string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := g.Result().Query(context.Background(), q, params)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func hasDiagnostic(r model.BuildReport, code string) bool {
	return slices.ContainsFunc(r.Diagnostics, func(d model.Diagnostic) bool { return d.Code == code })
}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func (g *Session) addDocumentsSync(ctx context.Context, docs ...extraction.Document) (model.BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}

func newTestSession(snapshot string, opts Options) (*Session, error) {
	b, err := NewBuilder(snapshot, opts)
	if err != nil {
		return nil, err
	}
	return NewSession(b)
}
func buildTestSession(ctx context.Context, snapshot string, docs []extraction.Document, opts Options) (*Session, model.BuildReport, error) {
	s, err := newTestSession(snapshot, opts)
	if err != nil {
		return nil, model.BuildReport{}, err
	}
	if err := s.AddDocuments(ctx, docs...); err != nil {
		return nil, s.Report(), err
	}
	r, err := s.Wait(ctx)
	return s, r, err
}
