package codegraph

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestDocumentsMatchFilesystem(t *testing.T) {
	source := fixture()
	source["worker.py"] = &fstest.MapFile{Data: []byte("def work():\n    pass\ndef entry():\n    work()\n")}
	source["web/app.ts"] = &fstest.MapFile{Data: []byte("function work() {}\nfunction entry() { work(); }\n")}
	paths := []string{"main.go", "helper.go", "lib/work.go", "entry_test.go", "worker.py", "web/app.ts"}
	var inputs []Document
	for _, path := range paths {
		inputs = append(inputs, Document{Path: path, Content: source[path].Data})
	}
	ctx := context.Background()
	opts := Options{ModulePath: "example.org/demo"}
	want, _, err := Build(ctx, "rev", documents(source, paths...), opts)
	if err != nil {
		t.Fatal(err)
	}
	g, err := New("rev", opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddDocuments(ctx, inputs...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Nodes(), want.Nodes()) || !reflect.DeepEqual(g.Relations(), want.Relations()) || !reflect.DeepEqual(g.Report(), want.Report()) {
		t.Fatal("document and filesystem inputs produced different graph facts or coverage")
	}
}

func TestDocumentsResolveAcrossBatchesAndInputForms(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	g, err := New("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	main := Document{Path: "main.go", Content: source["main.go"].Data}
	r, err := g.AddDocuments(ctx, main, main)
	if err != nil || r.Complete || !reflect.DeepEqual(r.Files, []string{"main.go"}) {
		t.Fatalf("explicit batch must not discover dependencies: %+v, %v", r, err)
	}
	r, err = g.AddDocuments(ctx, Document{Path: "lib/work.go", Content: source["lib/work.go"].Data})
	if err != nil || r.Complete {
		t.Fatal(r, err)
	}
	r, err = g.AddDocuments(ctx, Document{Path: "helper.go", Content: source["helper.go"].Data})
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	if got := query(t, g, `MATCH (:Function {name:'Entry'})-[:calls]->(n:Function {name:'Work'}) RETURN n`, nil); len(got) != 2 {
		t.Fatalf("cross-document calls missing: %v", got)
	}
	nodes, relations, report := g.Nodes(), g.Relations(), g.Report()
	if _, err := g.AddDocuments(ctx, main, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("re-adding files as documents changed identities or coverage")
	}
	if _, err := g.AddDocuments(ctx, Document{Path: "helper.go", Content: []byte("package app\nfunc Changed(){}")}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("filesystem identity not shared with documents: %v", err)
	}
	changed := fstest.MapFS{"main.go": {Data: []byte("package app\nfunc Changed(){}")}}
	if _, err := g.AddDocuments(ctx, Document{Path: "main.go", Content: changed["main.go"].Data}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("document identity not shared with files: %v", err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("conflicting input changed graph")
	}
}

func TestDocumentsOwnContent(t *testing.T) {
	ctx := context.Background()
	content := []byte("package app\nfunc Entry(){ Work() }\n")
	original := bytes.Clone(content)
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddDocuments(ctx, Document{Path: "main.go", Content: content}); err != nil {
		t.Fatal(err)
	}
	// A later rebuild must still use the submitted snapshot, not caller memory.
	for i := range content {
		content[i] = 'x'
	}
	r, err := g.AddDocuments(ctx,
		Document{Path: "work.go", Content: []byte("package app\nfunc Work(){}\n")},
		Document{Path: "main.go", Content: original},
	)
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	if got := query(t, g, `MATCH (:Function {name:'Entry'})-[:calls]->(n:Function {name:'Work'}) RETURN n`, nil); len(got) != 1 {
		t.Fatalf("caller mutation corrupted retained source: %v", got)
	}
}

func TestDocumentsRollback(t *testing.T) {
	seed := Document{Path: "seed.go", Content: []byte("package app\nfunc Seed(){}\n")}
	added := Document{Path: "added.go", Content: []byte("package app\nfunc Added(){}\n")}
	for _, tc := range []struct {
		name      string
		opts      Options
		documents []Document
		wantErr   error
		cancel    bool
	}{
		{name: "changed path", documents: []Document{added, {Path: seed.Path, Content: added.Content}}, wantErr: ErrSnapshotChanged},
		{name: "conflicting duplicates", documents: []Document{added, {Path: added.Path, Content: seed.Content}}, wantErr: ErrSnapshotChanged},
		{name: "file count", opts: Options{MaxFiles: 1}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "file bytes", opts: Options{MaxFileBytes: int64(len(seed.Content))}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "source bytes", opts: Options{MaxSourceBytes: int64(len(seed.Content) + len(added.Content) - 1)}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "node count", opts: Options{MaxNodes: 2}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "invalid path", documents: []Document{added, {Path: "../escape.go", Content: seed.Content}}},
		{name: "canceled", documents: []Document{added}, wantErr: context.Canceled, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := New("rev", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.AddDocuments(context.Background(), seed); err != nil {
				t.Fatal(err)
			}
			nodes, relations, report := g.Nodes(), g.Relations(), g.Report()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			r, err := g.AddDocuments(ctx, tc.documents...)
			if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) || !reflect.DeepEqual(report, r) {
				t.Fatal("failed batch changed published state")
			}
		})
	}
}

func TestDocumentsPartialCoverage(t *testing.T) {
	g, err := New("rev", Options{Scope: []string{"src"}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.AddDocuments(context.Background(),
		Document{Path: "src/good.go", Content: []byte("package app\nfunc Good(){}")},
		Document{Path: "src/bad.go", Content: []byte("package broken\nfunc (")},
		Document{Path: "src/unknown.codegraph-unknown", Content: []byte("unknown")},
		Document{Path: "outside.go", Content: []byte("package app")},
	)
	if err != nil || r.Complete || !reflect.DeepEqual(r.Files, []string{"src/good.go", "src/unknown.codegraph-unknown"}) {
		t.Fatal(r, err)
	}
	codes := map[string]string{}
	for _, d := range r.Diagnostics {
		codes[d.Location.Path] = d.Code
	}
	want := map[string]string{"src/bad.go": "parse_error", "src/unknown.codegraph-unknown": "unsupported_language", "outside.go": "out_of_scope"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("diagnostics = %v, want %v", codes, want)
	}
}
