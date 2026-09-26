package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

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

func TestSourceGraph(t *testing.T) {
	g := built(t, Options{})
	rows := query(t, g, `MATCH (f:Document)-[:contains]->(a:Function {name:'Entry'})-[r:calls]->(b:Function {name:'Work'}) RETURN f,r,b`, nil)
	if len(rows) != 2 {
		t.Fatalf("calls=%v", rows)
	}
	a, b := rows[0]["r"].(Relation), rows[1]["r"].(Relation)
	if a.ID == b.ID || a.Location.StartByte == b.Location.StartByte {
		t.Fatalf("parallel calls collapsed: %+v %+v", a, b)
	}
	if a.Confidence != Exact || a.Basis != "imported_function" {
		t.Fatal(a)
	}
	if rows[0]["f"].(Node).Kind != DocumentKind || rows[0]["b"].(Node).Name != "Work" {
		t.Fatal(rows)
	}
	properties := query(t, g, `MATCH (:Document)-[:contains]->(a:Function {name:'Entry'})-[r:calls]->(b:Function {name:'Work'}) RETURN r.id AS id, r.confidence AS confidence, properties(r) AS props`, nil)
	for _, row := range properties {
		if row["confidence"] != "exact" || row["id"] == nil {
			t.Fatal(row)
		}
		if row["props"].(map[string]any)["id"] != row["id"] {
			t.Fatal("relationship property projection lost identity", row)
		}
	}
	paths := query(t, g, `MATCH p=(a:Function {name:'Entry'})-[:calls*1..2]->(b:Function {name:'End'}) WHERE all(r IN relationships(p) WHERE r.confidence = $confidence) RETURN p, length(p) AS hops`, map[string]any{"confidence": "exact"})
	if len(paths) != 2 {
		t.Fatal(paths)
	}
	for _, row := range paths {
		p := row["p"].(Path)
		if len(p.Nodes) != 3 || len(p.Relations) != 2 || row["hops"] != int64(2) {
			t.Fatal(row)
		}
	}
	cycles := query(t, g, `MATCH p=(a:Function {name:'helper'})-[:calls*1..3]->(a) RETURN p`, nil)
	if len(cycles) != 1 {
		t.Fatal(cycles)
	}
	imports := query(t, g, `MATCH (f:Document {path:'main.go'})-[r:imports]->(d:Document) RETURN d.path AS path`, nil)
	if len(imports) != 1 || imports[0]["path"] != "lib/work.go" {
		t.Fatal(imports)
	}
	tests := query(t, g, `MATCH (t:Function)-[:calls*1..3]->(b:Function {name:'End'}) WHERE 'case' IN t.markers RETURN DISTINCT t.name AS name`, nil)
	if len(tests) != 2 {
		t.Fatal(tests)
	} // Entry itself and TestEntry both have case markers.
}

func TestMarkerRoundTrip(t *testing.T) {
	g := built(t, Options{})
	rows := query(t, g, `MATCH (n:Function {name:'Entry'}) RETURN n, n.spec AS specs, n.markerData AS data`, nil)
	n := rows[0]["n"].(Node)
	if len(n.Markers) != 5 {
		t.Fatal(n)
	}
	var markers []Marker
	if err := json.Unmarshal([]byte(rows[0]["data"].(string)), &markers); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(markers, n.Markers) {
		t.Fatal(markers, n.Markers)
	}
	if n.Markers[0].Location.Line != 3 || n.Markers[0].Text != "review keeps source evidence" {
		t.Fatal(n.Markers)
	}
	n.Markers[0].Text = "mutated"
	for _, n := range g.Nodes() {
		for _, m := range n.Markers {
			if m.Text == "mutated" {
				t.Fatal("query result aliases graph")
			}
		}
	}
}

func TestExtendAndSnapshotIdentity(t *testing.T) {
	ctx := context.Background()
	source := fixture()
	g, r, err := Build(ctx, "rev-A", documents(source, "main.go"), Options{ModulePath: "example.org/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) == 0 {
		t.Fatal("unloaded imports/calls reported complete")
	}
	r, err = g.addDocumentsSync(ctx, documents(source, "helper.go", "lib/work.go")...)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	nodes, edges := g.Nodes(), g.Relations()
	r, err = g.addDocumentsSync(ctx, documents(source, "main.go", "helper.go", "lib/work.go")...)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(edges, g.Relations()) {
		t.Fatal("re-add changed identities")
	}
	source["main.go"] = &fstest.MapFile{Data: []byte("package app\nfunc Changed(){}")}
	if _, err = g.addDocumentsSync(ctx, documents(source, "main.go")...); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nodes, g.Nodes()) {
		t.Fatal("failed batch changed graph")
	}
	fresh, _, err := Build(ctx, "rev-B", documents(source, "main.go"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(query(t, fresh, `MATCH (n:Function {name:'Changed'}) RETURN n`, nil)) != 1 {
		t.Fatal("new snapshot not built")
	}
}

func TestExpansionAndScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scope    []string
		files    int
		complete bool
	}{
		{"local", nil, 3, true}, {"restricted", []string{"main.go", "helper.go"}, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, r, err := Build(context.Background(), "rev", documents(fixture(), "main.go", "helper.go", "lib/work.go"), Options{ModulePath: "example.org/demo", Scope: tc.scope})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Documents) != tc.files || (len(r.Diagnostics) == 0) != tc.complete {
				t.Fatal(r)
			}
			if len(g.Nodes()) == 0 {
				t.Fatal("empty")
			}
		})
	}
}

func TestBuildFailuresAndBudget(t *testing.T) {
	ctx := context.Background()
	g := built(t, Options{})
	bad := fstest.MapFS{"bad.go": {Data: []byte("package broken\nfunc (")}, "script.unknown-codegraph": {Data: []byte("def hello(): pass")}}
	r, err := g.addDocumentsSync(ctx, documents(bad, "bad.go", "script.unknown-codegraph")...)
	if err != nil {
		t.Fatal(err)
	}
	if (len(r.Diagnostics) == 0) || len(r.Diagnostics) != 2 {
		t.Fatal(r)
	}
	if _, err := g.addDocumentsSync(ctx, Document{Path: "../outside.go", Content: []byte("package p")}); err == nil {
		t.Fatal("path traversal accepted")
	}
	g2, err := New("rev", Options{MaxDocuments: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g2.addDocumentsSync(ctx, documents(fixture(), "main.go", "helper.go")...); !errors.Is(err, ErrBuildBudget) {
		t.Fatal(err)
	}
	if len(g2.Nodes()) != 0 {
		t.Fatal("budget published partial graph")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = g2.addDocumentsSync(ctx, documents(fixture(), "main.go")...); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestShadowingAndAmbiguity(t *testing.T) {
	source := fstest.MapFS{"a.go": {Data: []byte("package p\nfunc Target(){}\nfunc Entry(Target func()){Target()}\nfunc Invoke(){Dup()}\nfunc Dup(){}")}, "b.go": {Data: []byte("package p\nfunc Dup(){}")}}
	g, r, err := Build(context.Background(), "rev", documents(source, "a.go", "b.go"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) == 0 {
		t.Fatal(r)
	}
	if len(query(t, g, `MATCH (:Function {name:'Entry'})-[:calls]->(n) RETURN n`, nil)) != 0 {
		t.Fatal("callback bound to package function")
	}
	rows := query(t, g, `MATCH (:Function {name:'Invoke'})-[r:calls]->(n) RETURN r.confidence AS confidence`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r["confidence"] != "candidate" {
			t.Fatal(rows)
		}
	}
}

func TestQueryBoundary(t *testing.T) {
	g := built(t, Options{})
	for _, q := range []string{`CREATE (:Function)`, `MATCH (n) DELETE n`, `MATCH (n) SET n.name='oops'`, `CALL db.labels()`} {
		if _, err := g.Query(context.Background(), q, nil); !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: %v", q, err)
		}
	}
	for _, q := range []string{
		`MATCH p=(a)-[*]->(b) RETURN p`,
		`MATCH p=(a)-[*1..99]->(b) RETURN p`,
		`MATCH (n) RETURN [(n)-[*]->(m) | m]`,
		`MATCH (n) WHERE EXISTS { MATCH (n)-[*]->(m) } RETURN n`,
	} {
		if _, err := g.Query(context.Background(), q, nil); !errors.Is(err, ErrQueryBudget) {
			t.Errorf("%s: %v", q, err)
		}
	}
	rows := query(t, g, `RETURN 'CREATE (x)-[*]->(y)' AS literal`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Query(ctx, `MATCH (n) RETURN n`, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	limited := built(t, Options{MaxResultRows: 1})
	rows, err := limited.Query(context.Background(), `MATCH (n) RETURN n`, nil)
	if !errors.Is(err, ErrQueryBudget) || rows != nil {
		t.Fatal(rows, err)
	}
}

func TestConcurrentReadersAndExtension(t *testing.T) {
	g := built(t, Options{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 5 {
				if _, err := g.Query(context.Background(), `MATCH (n:Function) RETURN n`, nil); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Go(func() {
		_, err := g.addDocumentsSync(context.Background(), documents(fixture(), "main.go")...)
		if err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
}

func TestOptions(t *testing.T) {
	if _, err := New("", Options{}); err == nil {
		t.Fatal("empty snapshot accepted")
	}
	if _, err := New("x", Options{MaxDocuments: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
	if _, err := New("x", Options{Scope: []string{"../"}}); err == nil {
		t.Fatal("invalid scope accepted")
	}
	if !fs.ValidPath("main.go") || !strings.HasPrefix(DocumentID("main.go"), "document:") {
		t.Fatal("file identity")
	}
}

func TestAllBuildBudgetsRollback(t *testing.T) {
	source := fstest.MapFS{
		"main.go": {Data: []byte("package p\nimport \"demo/a\"\nfunc Entry(){a.Work()}")},
		"a/a.go":  {Data: []byte("package a\nimport \"demo/b\"\nfunc Work(){b.End()}")},
		"b/b.go":  {Data: []byte("package b\nfunc End(){}")},
	}
	for _, opts := range []Options{
		{MaxDocumentBytes: 10}, {MaxSourceBytes: 10}, {MaxNodes: 1}, {MaxRelations: 1},
	} {
		g, err := New("rev", opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = g.addDocumentsSync(context.Background(), documents(source, "main.go", "a/a.go", "b/b.go")...); !errors.Is(err, ErrBuildBudget) {
			t.Fatalf("opts=%+v err=%v", opts, err)
		}
		if len(g.Nodes()) != 0 {
			t.Fatal("budget published data")
		}
	}
}

func TestMethodsClosuresAndGenericCalls(t *testing.T) {
	source := fstest.MapFS{"source.go": {Data: []byte(`package p
// +spec=` + "`generic type`" + `
type Box[T any] struct{}
// +rule=` + "`method rule`" + `
func (b *Box[T]) Run(){ Work[int]() }
func Work[T any](){}
func Entry(){ callback:=func(){ Work[int]() }; callback() }
`)}}
	g, r, err := Build(context.Background(), "rev", documents(source, "source.go"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) == 0 {
		t.Fatal("closure gap hidden")
	}
	rows := query(t, g, `MATCH (n:Method {qualifiedName:'Box.Run'}) RETURN n`, nil)
	if len(rows) != 1 || len(rows[0]["n"].(Node).Markers) != 1 {
		t.Fatal(rows)
	}
	if len(query(t, g, `MATCH (:Method {name:'Run'})-[:calls]->(:Function {name:'Work'}) RETURN 1`, nil)) != 1 {
		t.Fatal("generic call unresolved")
	}
	if len(query(t, g, `MATCH (:Function {name:'Entry'})-[:calls]->(n) RETURN n`, nil)) != 0 {
		t.Fatal("closure body attributed to outer function")
	}
}
