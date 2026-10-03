package codegraph

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
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
	g, err := NewBuilder("rev", opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.addDocumentsSync(ctx, inputs...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Result().Nodes(), want.Nodes()) || !reflect.DeepEqual(g.Result().Relations(), want.Relations()) || !reflect.DeepEqual(g.Report(), want.Report()) {
		t.Fatal("document and filesystem inputs produced different graph facts or coverage")
	}
}

func TestDocumentsResolveAcrossBatchesAndInputForms(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	g, err := NewBuilder("rev", Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	main := Document{Path: "main.go", Content: source["main.go"].Data}
	r, err := g.addDocumentsSync(ctx, main, main)
	if err != nil || (len(r.Diagnostics) == 0) || !reflect.DeepEqual(r.Documents, []string{"main.go"}) {
		t.Fatalf("explicit batch must not discover dependencies: %+v, %v", r, err)
	}
	r, err = g.addDocumentsSync(ctx, Document{Path: "lib/work.go", Content: source["lib/work.go"].Data})
	if err != nil || (len(r.Diagnostics) == 0) {
		t.Fatal(r, err)
	}
	r, err = g.addDocumentsSync(ctx, Document{Path: "helper.go", Content: source["helper.go"].Data})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	if got := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[:calls]->(n:Function {name:'Work'}) RETURN n`, nil); len(got) != 2 {
		t.Fatalf("cross-document calls missing: %v", got)
	}
	nodes, relations, report := g.Result().Nodes(), g.Result().Relations(), g.Report()
	if _, err := g.addDocumentsSync(ctx, main, Document{Path: "helper.go", Content: source["helper.go"].Data}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("re-adding files as documents changed identities or coverage")
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "helper.go", Content: []byte("package app\nfunc Changed(){}")}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("filesystem identity not shared with documents: %v", err)
	}
	changed := fstest.MapFS{"main.go": {Data: []byte("package app\nfunc Changed(){}")}}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "main.go", Content: changed["main.go"].Data}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("document identity not shared with files: %v", err)
	}
	if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("conflicting input changed graph")
	}
}

func TestDocumentsOwnContent(t *testing.T) {
	ctx := context.Background()
	content := []byte("package app\nfunc Entry(){ Work() }\n")
	original := bytes.Clone(content)
	g, err := NewBuilder("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "main.go", Content: content}); err != nil {
		t.Fatal(err)
	}
	// A later rebuild must still use the submitted snapshot, not caller memory.
	for i := range content {
		content[i] = 'x'
	}
	r, err := g.addDocumentsSync(ctx,
		Document{Path: "work.go", Content: []byte("package app\nfunc Work(){}\n")},
		Document{Path: "main.go", Content: original},
	)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	if got := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[:calls]->(n:Function {name:'Work'}) RETURN n`, nil); len(got) != 1 {
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
		{name: "file count", opts: Options{MaxDocuments: 1}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "file bytes", opts: Options{MaxDocumentBytes: int64(len(seed.Content))}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "source bytes", opts: Options{MaxSourceBytes: int64(len(seed.Content) + len(added.Content) - 1)}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "node count", opts: Options{MaxNodes: 3}, documents: []Document{added}, wantErr: ErrBuildBudget},
		{name: "invalid path", documents: []Document{added, {Path: "../escape.go", Content: seed.Content}}},
		{name: "canceled", documents: []Document{added}, wantErr: context.Canceled, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewBuilder("rev", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.addDocumentsSync(context.Background(), seed); err != nil {
				t.Fatal(err)
			}
			nodes, relations, report := g.Result().Nodes(), g.Result().Relations(), g.Report()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			r, err := g.addDocumentsSync(ctx, tc.documents...)
			if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) || !reflect.DeepEqual(report, r) {
				t.Fatal("failed batch changed published state")
			}
		})
	}
}

func TestDocumentsPartialCoverage(t *testing.T) {
	g, err := NewBuilder("rev", Options{Scope: []string{"src"}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.addDocumentsSync(context.Background(),
		Document{Path: "src/good.go", Content: []byte("package app\nfunc Good(){}")},
		Document{Path: "src/bad.go", Content: []byte("package broken\nfunc (")},
		Document{Path: "src/unknown.codegraph-unknown", Content: []byte("unknown")},
		Document{Path: "outside.go", Content: []byte("package app")},
	)
	if err != nil || (len(r.Diagnostics) == 0) || !reflect.DeepEqual(r.Documents, []string{"src/good.go", "src/unknown.codegraph-unknown"}) {
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

func TestDocumentOnlyDocumentsEnterGraph(t *testing.T) {
	g, r, err := Build(context.Background(), "rev", []Document{
		{Path: "main.go", Content: []byte("package app\nfunc Entry(){}\n")},
		{Path: "notes.cg-unrecognized", Content: []byte("plain text notes\n")},
	}, Options{})
	if err != nil || (len(r.Diagnostics) == 0) {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(r.Documents, []string{"main.go", "notes.cg-unrecognized"}) {
		t.Fatalf("file-only document missing from coverage: %v", r.Documents)
	}
	if !hasDiagnostic(r, "unsupported_language") {
		t.Fatalf("coverage gap not diagnosable: %+v", r.Diagnostics)
	}
	rows := query(t, g, `MATCH (f:Document {path:'notes.cg-unrecognized'}) RETURN f`, nil)
	if len(rows) != 1 {
		t.Fatalf("file node = %v", rows)
	}
	f := rows[0]["f"].(Node)
	if f.Kind != DocumentKind || f.Language != "" || f.Name != "notes.cg-unrecognized" {
		t.Fatalf("file-only node = %+v", f)
	}
	if got := query(t, g, `MATCH (:Document {path:'notes.cg-unrecognized'})-[:declares]->(n) RETURN n`, nil); len(got) != 0 {
		t.Fatalf("file-only document parsed declarations: %v", got)
	}
	if got := query(t, g, `MATCH (f:Document) RETURN f`, nil); len(got) != 2 {
		t.Fatalf("mixed batch file nodes = %v", got)
	}
	if got := query(t, g, `MATCH (:Document {path:'main.go'})-[:declares]->(n:Function {name:'Entry'}) RETURN n`, nil); len(got) != 1 {
		t.Fatalf("grammar-backed document lost extraction: %v", got)
	}
}

func TestDocumentOnlyDocumentsIdentity(t *testing.T) {
	ctx := context.Background()
	g, err := NewBuilder("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	notes := Document{Path: "notes.cg-unrecognized", Content: []byte("plain text notes")}
	if _, err := g.addDocumentsSync(ctx, notes); err != nil {
		t.Fatal(err)
	}
	nodes, relations, report := g.Result().Nodes(), g.Result().Relations(), g.Report()
	if _, err := g.addDocumentsSync(ctx, notes, notes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("re-adding a file-only document changed identities or coverage")
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: notes.Path, Content: []byte("changed notes")}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("conflicting file-only content = %v", err)
	}
	if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) {
		t.Fatal("conflicting input changed graph")
	}
}

func TestDocumentOnlyDocumentsOwnContent(t *testing.T) {
	ctx := context.Background()
	content := []byte("original notes")
	g, err := NewBuilder("rev", Options{})
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

func TestDocumentOnlyDocumentsBudget(t *testing.T) {
	seed := Document{Path: "seed.go", Content: []byte("package app\nfunc Seed(){}\n")}
	notes := Document{Path: "notes.cg-unrecognized", Content: []byte("plain text notes longer than the seed file")}
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"file count", Options{MaxDocuments: 1}},
		{"file bytes", Options{MaxDocumentBytes: int64(len(seed.Content))}},
		{"source bytes", Options{MaxSourceBytes: int64(len(seed.Content) + len(notes.Content) - 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewBuilder("rev", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := g.addDocumentsSync(context.Background(), seed); err != nil {
				t.Fatal(err)
			}
			nodes, relations, report := g.Result().Nodes(), g.Result().Relations(), g.Report()
			if _, err := g.addDocumentsSync(context.Background(), notes); !errors.Is(err, ErrBuildBudget) {
				t.Fatalf("file-only document bypassed budget: %v", err)
			}
			if !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(relations, g.Result().Relations()) || !reflect.DeepEqual(report, g.Report()) {
				t.Fatal("failed batch changed published state")
			}
		})
	}
}

const gitlinkCommit = "0123456789abcdef0123456789abcdef01234567"

// +case=`A supplied gitlink is one versioned Document, regardless of filename or checkout availability`
func TestGitlinkDocumentIdentity(t *testing.T) {
	ctx := context.Background()
	g, err := NewBuilder("parent", Options{})
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Path: "sdk.ts", Gitlink: gitlinkCommit}
	// A previous exploration at the same path must not poison the typed cache.
	if _, err := g.Extract(ctx, Document{Path: document.Path, Content: []byte("function wrong() {}")}); err != nil {
		t.Fatal(err)
	}
	facts, err := g.AddDocument(ctx, document).Wait()
	if err != nil || facts.Gitlink != gitlinkCommit || facts.Language != "" || len(facts.Declarations) != 0 || len(facts.Issues) != 0 {
		t.Fatal(facts, err)
	}
	report, err := g.Wait(ctx)
	if err != nil || len(report.Diagnostics) != 0 || len(g.Result().Nodes()) != 1 || len(g.Result().Relations()) != 0 {
		t.Fatal(report, err, g.Result().Nodes())
	}
	node, ok := g.Result().Node(document.ID())
	if !ok || node.Kind != DocumentKind || node.Gitlink != gitlinkCommit || node.Location.Path != document.Path || node.Location.EndByte != 0 {
		t.Fatal(node)
	}
	rows := query(t, g.Result(), `MATCH (n:Document {gitlink:$commit}) RETURN n,n.gitlink AS revision`, map[string]any{"commit": gitlinkCommit})
	if len(rows) != 1 || !reflect.DeepEqual(rows[0]["n"], node) || rows[0]["revision"] != gitlinkCommit {
		t.Fatal(rows)
	}
	if err := g.AddDocuments(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []Document{{Path: document.Path, Gitlink: strings.Repeat("a", 40)}, {Path: document.Path}} {
		if err := g.AddDocuments(ctx, changed); !errors.Is(err, ErrSnapshotChanged) {
			t.Fatal("changed material accepted", err)
		}
	}
	fresh, err := g.Extract(ctx, Document{Path: document.Path, Gitlink: strings.Repeat("b", 64)})
	if err != nil || fresh.Gitlink != strings.Repeat("b", 64) {
		t.Fatal("exploration reused another revision", fresh, err)
	}
	still, _ := g.Result().Node(document.ID())
	if still.Gitlink != gitlinkCommit {
		t.Fatal("exploration changed published graph", still)
	}
}

// +case=`Imports stop at the supplied gitlink boundary; no child module or callable is fabricated`
func TestGitlinkImports(t *testing.T) {
	for _, tc := range []struct {
		path, source, root string
		opts               Options
		confidence         Confidence
	}{
		{"src/app.ts", "import {work} from '../sdk/src/api'; export function run(){work()}", "sdk", Options{}, Exact},
		{"src/app.js", "import '../sdk';", "sdk", Options{}, Exact},
		{"src/app.tsx", "import '../sdk/lib';", "sdk", Options{}, Exact},
		{"app.py", "import sdk.api\nsdk.api.work()\n", "sdk", Options{}, Scoped},
		{"pkg/app.py", "from .sdk.api import work\nwork()\n", "pkg/sdk", Options{}, Exact},
		{"app.go", "package app\nimport sdk \"example.org/app/sdk/api\"\nfunc Run(){sdk.Work()}\n", "sdk", Options{ModulePath: "example.org/app"}, Exact},
	} {
		t.Run(tc.path, func(t *testing.T) {
			source := Document{Path: tc.path, Content: []byte(tc.source)}
			gitlink := Document{Path: tc.root, Gitlink: gitlinkCommit}
			g, report, err := Build(context.Background(), "parent", []Document{source, gitlink}, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, edge := range g.Relations() {
				if edge.Target == gitlink.ID() && edge.Source == source.ID() {
					if edge.Kind != Imports || edge.Source != source.ID() || edge.Confidence != tc.confidence || edge.Evidence[0].Basis != "gitlink_boundary" || edge.Location.Path != tc.path || edge.Location.EndByte <= edge.Location.StartByte {
						t.Fatal(edge)
					}
					count++
				}
				if edge.Kind == Calls {
					t.Fatal("invented child callable", edge)
				}
			}
			if count != 1 {
				t.Fatal("missing gitlink edge", g.Relations(), report)
			}
			for _, node := range g.Nodes() {
				if node.Location != nil && strings.HasPrefix(node.Location.Path, tc.root+"/") {
					t.Fatal("invented child source", node)
				}
			}
			for _, d := range report.Diagnostics {
				if d.Location.Path == tc.root || d.Code == "unresolved_import" {
					t.Fatal("gitlink reported as unsupported or missing", d)
				}
			}
		})
	}
}

func TestGitlinkImportPathBoundaries(t *testing.T) {
	g, report, err := Build(context.Background(), "parent", []Document{
		{Path: "app.ts", Content: []byte("import './sdk-extra/a'; import '@example/sdk'; import './sdk/a'; import './sdk/b';")},
		{Path: "sdk", Gitlink: gitlinkCommit},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	edges := g.RelationsFrom(DocumentID("app.ts"), Imports)
	if len(edges) != 2 {
		t.Fatal(edges)
	}
	gaps := 0
	for _, d := range report.Diagnostics {
		if d.Code == "unresolved_import" {
			gaps++
		}
	}
	if gaps != 2 {
		t.Fatal("guessed a package alias or crossed a path segment", report)
	}
}

func TestGitlinkBatchAndBudgets(t *testing.T) {
	ctx := context.Background()
	gitlink := Document{Path: "sdk", Gitlink: gitlinkCommit}
	source := Document{Path: "app.ts", Content: []byte("import './sdk/api';")}
	g, _, err := buildTestBuilder(ctx, "parent", []Document{source}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.AddDocuments(ctx, gitlink); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	all, _, err := Build(ctx, "parent", []Document{gitlink, source}, Options{})
	if err != nil || !reflect.DeepEqual(g.Result().Nodes(), all.Nodes()) || !reflect.DeepEqual(g.Result().Relations(), all.Relations()) {
		t.Fatal("batch order changed graph", err)
	}
	for _, options := range []Options{{MaxDocumentBytes: 39}, {MaxSourceBytes: 79}, {MaxDocuments: 1}, {MaxNodes: 1}} {
		g, err := NewBuilder("limited", options)
		if err != nil {
			t.Fatal(err)
		}
		err = g.AddDocuments(ctx, gitlink, Document{Path: "other", Gitlink: gitlinkCommit})
		if err == nil {
			_, err = g.Wait(ctx)
		}
		if !errors.Is(err, ErrBuildBudget) || len(g.Result().Nodes()) != 0 {
			t.Fatal("gitlink bypassed budget or partial publication", options, err, g.Result().Nodes())
		}
	}
}

func TestGitlinkInvalidAndOverlappingMaterials(t *testing.T) {
	ctx := context.Background()
	for _, bad := range []Document{
		{Path: ".", Gitlink: gitlinkCommit},
		{Path: "sdk", Gitlink: "short"},
		{Path: "sdk", Gitlink: strings.Repeat("z", 40)},
		{Path: "sdk", Gitlink: gitlinkCommit, Content: []byte("source")},
	} {
		g, _ := NewBuilder("invalid", Options{})
		if _, err := g.Extract(ctx, bad); err == nil {
			t.Fatal("invalid extraction accepted", bad)
		}
		if err := g.AddDocuments(ctx, Document{Path: "ok.ts", Content: []byte("function ok(){}")}, bad); err == nil {
			t.Fatal("invalid batch accepted", err)
		}
		if _, err := g.GetDocument(DocumentID("ok.ts")); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatal("invalid batch partially admitted", err)
		}
	}
	link := Document{Path: "sdk", Gitlink: gitlinkCommit}
	child := Document{Path: "sdk/api.ts", Content: []byte("export function work(){}")}
	for _, docs := range [][]Document{{link, child}, {child, link}, {link, {Path: "sdk/nested", Gitlink: gitlinkCommit}}} {
		g, _ := NewBuilder("overlap", Options{})
		if err := g.AddDocuments(ctx, docs...); err == nil {
			t.Fatal("mixed snapshots admitted", err)
		}
		for _, doc := range docs {
			if _, err := g.GetDocument(doc.ID()); !errors.Is(err, ErrDocumentNotFound) {
				t.Fatal("overlapping batch partially admitted", err)
			}
		}
	}
	for _, docs := range [][]Document{{link, child}, {child, link}} {
		g, _ := NewBuilder("batches", Options{})
		if err := g.AddDocuments(ctx, docs[0]); err != nil {
			t.Fatal(err)
		}
		if err := g.AddDocuments(ctx, docs[1]); err == nil {
			t.Fatal("in-flight overlap admitted")
		}
		if _, err := g.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		if err := g.AddDocuments(ctx, docs[1]); err == nil {
			t.Fatal("published overlap admitted")
		}
	}
}
