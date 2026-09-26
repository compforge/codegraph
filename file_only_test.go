package codegraph

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFileOnlyDocumentsEnterGraph(t *testing.T) {
	g, r, err := Build(context.Background(), "rev", []Document{
		{Path: "main.go", Content: []byte("package app\nfunc Entry(){}\n")},
		{Path: "notes.cg-unrecognized", Content: []byte("plain text notes\n")},
	}, Options{})
	if err != nil || (len(r.Diagnostics) == 0) {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(r.Files, []string{"main.go", "notes.cg-unrecognized"}) {
		t.Fatalf("file-only document missing from coverage: %v", r.Files)
	}
	if !hasDiagnostic(r, "unsupported_language") {
		t.Fatalf("coverage gap not diagnosable: %+v", r.Diagnostics)
	}
	rows := query(t, g, `MATCH (f:File {path:'notes.cg-unrecognized'}) RETURN f`, nil)
	if len(rows) != 1 {
		t.Fatalf("file node = %v", rows)
	}
	f := rows[0]["f"].(Node)
	if f.Kind != File || f.Language != "" || f.Name != "notes.cg-unrecognized" {
		t.Fatalf("file-only node = %+v", f)
	}
	if got := query(t, g, `MATCH (:File {path:'notes.cg-unrecognized'})-[:contains]->(n) RETURN n`, nil); len(got) != 0 {
		t.Fatalf("file-only document parsed declarations: %v", got)
	}
	if got := query(t, g, `MATCH (f:File) RETURN f`, nil); len(got) != 2 {
		t.Fatalf("mixed batch file nodes = %v", got)
	}
	if got := query(t, g, `MATCH (:File {path:'main.go'})-[:contains]->(n:Function {name:'Entry'}) RETURN n`, nil); len(got) != 1 {
		t.Fatalf("grammar-backed document lost extraction: %v", got)
	}
}

func TestFileOnlyDocumentsIdentity(t *testing.T) {
	ctx := context.Background()
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	notes := Document{Path: "notes.cg-unrecognized", Content: []byte("plain text notes")}
	if _, err := g.addDocumentsSync(ctx, notes); err != nil {
		t.Fatal(err)
	}
	nodes, relations, report := g.Nodes(), g.Relations(), g.Report()
	if _, err := g.addDocumentsSync(ctx, notes, notes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("re-adding a file-only document changed identities or coverage")
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: notes.Path, Content: []byte("changed notes")}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("conflicting file-only content = %v", err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("conflicting input changed graph")
	}
}

func TestFileOnlyDocumentsOwnContent(t *testing.T) {
	ctx := context.Background()
	content := []byte("original notes")
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "notes.cg-unrecognized", Content: content}); err != nil {
		t.Fatal(err)
	}
	for i := range content {
		content[i] = 'x'
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "notes.cg-unrecognized", Content: []byte("original notes")}); err != nil {
		t.Fatalf("caller mutation corrupted retained source: %v", err)
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "notes.cg-unrecognized", Content: content}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("mutated caller memory matched retained source: %v", err)
	}
}

func TestFileOnlyDocumentsBudget(t *testing.T) {
	seed := Document{Path: "seed.go", Content: []byte("package app\nfunc Seed(){}\n")}
	notes := Document{Path: "notes.cg-unrecognized", Content: []byte("plain text notes longer than the seed file")}
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"file count", Options{MaxFiles: 1}},
		{"file bytes", Options{MaxFileBytes: int64(len(seed.Content))}},
		{"source bytes", Options{MaxSourceBytes: int64(len(seed.Content) + len(notes.Content) - 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := New("rev", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.addDocumentsSync(context.Background(), seed); err != nil {
				t.Fatal(err)
			}
			nodes, relations, report := g.Nodes(), g.Relations(), g.Report()
			if _, err := g.addDocumentsSync(context.Background(), notes); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("file-only document bypassed budget: %v", err)
			}
			if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) || !reflect.DeepEqual(report, g.Report()) {
				t.Fatal("failed batch changed published state")
			}
		})
	}
}
