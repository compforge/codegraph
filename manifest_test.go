package codegraph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestManifestMetadata(t *testing.T) {
	for _, tc := range []struct {
		path, source, format, name, version string
		project, build, workspace           bool
	}{
		{"go.mod", "module example.org/app\n\ngo 1.24\nrequire example.org/lib v1.0.0\n", "gomod", "example.org/app", "", true, false, false},
		{"nested/go.mod", "module \"example.org/quoted\"\n", "gomod", "example.org/quoted", "", true, false, false},
		{"pyproject.toml", "[project]\nname = \"demo\"\nversion = '1.0'\n[build-system]\nrequires=[]\n[tool.uv.workspace]\nmembers=['lib']\n", "pyproject", "demo", "1.0", true, true, true},
		{"nested/pyproject.toml", "[tool.ruff]\nline-length = 100\n", "pyproject", "", "", false, false, false},
		{"pyproject.toml", "[\"project\"]\n'name' = \"demo\"\nversion = \"1.0\"\n", "pyproject", "demo", "1.0", true, false, false},
		{"pyproject.toml", "project.name = 'demo'\nproject.version = '1.0'\n", "pyproject", "demo", "1.0", true, false, false},
		{"pyproject.toml", "project = {name = 'demo', version = '1.0'}\n", "pyproject", "demo", "1.0", true, false, false},
		{"pyproject.toml", "[project]\nname='demo'\ndynamic=['version']\n[tool.other]\nversion='wrong'\n", "pyproject", "demo", "", true, false, false},
		{"package.json", `{"name":"@app/demo","version":"1.0","workspaces":["lib"],"dependencies":{"name":"wrong"}}`, "package_json", "@app/demo", "1.0", true, false, true},
		{"package.json", `{"name":"dem\u006f","version":"1.0"}`, "package_json", "demo", "1.0", true, false, false},
	} {
		t.Run(tc.path+"/"+tc.name+"/"+tc.source, func(t *testing.T) {
			g, report, err := Build(context.Background(), "snapshot", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil || len(report.Diagnostics) != 0 {
				t.Fatalf("%v %+v", err, report.Diagnostics)
			}
			d, ok := g.Document(tc.path)
			if !ok || d.Kind != DocumentNodeKind || !slices.Contains(d.Tags, ManifestTag) || d.Manifest == nil {
				t.Fatalf("%+v", d)
			}
			m := d.Manifest
			if m.Format != tc.format || m.Name != tc.name || m.Version != tc.version || m.Project != tc.project || m.BuildSystem != tc.build || m.Workspace != tc.workspace {
				t.Fatalf("%+v", m)
			}
			if tc.name != "" && (m.NameLocation == nil || m.NameLocation.Path != tc.path || m.NameLocation.EndByte <= m.NameLocation.StartByte) {
				t.Fatalf("missing name provenance: %+v", m)
			}
			rows := query(t, g, `MATCH (d:Document) WHERE 'manifest' IN d.tags RETURN d, d.manifestFormat AS format, d.manifestName AS name, d.manifestVersion AS version, d.manifestProject AS project, d.manifestBuildSystem AS build, d.manifestWorkspace AS workspace`, nil)
			if len(rows) != 1 || rows[0]["format"] != tc.format || rows[0]["name"] != tc.name || rows[0]["version"] != tc.version || rows[0]["project"] != tc.project || rows[0]["build"] != tc.build || rows[0]["workspace"] != tc.workspace {
				t.Fatal(rows)
			}
			if !reflect.DeepEqual(rows[0]["d"].(Node), d.Node) {
				t.Fatal("typed/Cypher node disagreement")
			}
			if tc.format != "gomod" && len(query(t, g, `MATCH (n) WHERE n.kind <> 'Document' AND n.kind <> 'Directory' RETURN n`, nil)) != 0 {
				t.Fatal("packaging project invented an import namespace", g.Nodes())
			}
		})
	}
}

func TestManifestTOMLSectionIdentity(t *testing.T) {
	for _, tc := range []struct {
		source                    string
		project, build, workspace bool
	}{
		{"[\"tool.uv.workspace\"]\nmembers=[]\n", false, false, false},
		{"[project.urls]\nhomepage='https://example.org'\n", true, false, false},
		{"build-system.requires=[]\ntool.uv.workspace.members=[]\n", false, true, true},
	} {
		g, r, err := Build(context.Background(), "s", []Document{{Path: "pyproject.toml", Content: []byte(tc.source)}}, Options{})
		if err != nil || len(r.Diagnostics) != 0 {
			t.Fatal(err, r)
		}
		d, _ := g.Document("pyproject.toml")
		if d.Manifest.Project != tc.project || d.Manifest.BuildSystem != tc.build || d.Manifest.Workspace != tc.workspace {
			t.Fatalf("%q: %+v", tc.source, d.Manifest)
		}
	}
}

func TestDocumentKindsAndParseFailures(t *testing.T) {
	docs := []Document{
		{Path: "a.go", Content: []byte("package p\nfunc F() {}\n")},
		{Path: "bad/go.mod", Content: []byte("module (\n")},
		{Path: "bad/pyproject.toml", Content: []byte("[project\n")},
		{Path: "bad/package.json", Content: []byte("{\"name\":")},
		{Path: "input.json", Content: []byte(`{"name":"test data"}`)},
		{Path: "settings.toml", Content: []byte("name='settings'\n")},
		{Path: "readme.unknown", Content: []byte("data")},
		{Path: "vendor/sdk", Gitlink: strings.Repeat("a", 40)},
	}
	g, report, err := Build(context.Background(), "snapshot", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []DocumentKind{SourceDocument, UnknownDocument, UnknownDocument, UnknownDocument, UnknownDocument, UnknownDocument, UnknownDocument, GitlinkDocument}
	for i, doc := range docs {
		n, ok := g.Document(doc.Path)
		if !ok || n.DocumentKind != want[i] {
			t.Fatalf("%s: %+v", doc.Path, n)
		}
		if slices.Contains(n.Tags, ManifestTag) != (i >= 1 && i <= 3) {
			t.Fatalf("manifest tags: %s: %v", doc.Path, n.Tags)
		}
		if n.Manifest != nil {
			t.Fatal("metadata from failed/unrecognized manifest", n)
		}
	}
	if !hasDiagnostic(report, "parse_error") {
		t.Fatal(report)
	}
	if _, ok := g.Document("absent.go"); ok {
		t.Fatal("invented document")
	}
}

func TestManifestInvalidMetadata(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"package.json", `{"name":"one","name":"two"}`},
		{"package.json", `{"name":3}`},
		{"package.json", `[]`},
		{"pyproject.toml", "[project]\nname=3\n"},
		{"go.mod", "module example.org/one\nmodule example.org/two\n"},
		{"go.mod", "go 1.24\n"},
	} {
		g, r, err := Build(context.Background(), "s", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
		if err != nil || len(r.Diagnostics) == 0 {
			t.Fatalf("%s: %v %+v", tc.source, err, r)
		}
		d, ok := g.Document(tc.path)
		if !ok || !slices.Contains(d.Tags, ManifestTag) || d.Manifest != nil && d.Manifest.Name != "" {
			t.Fatal(d)
		}
		for _, r := range g.Relations() {
			if r.Kind != InDirectory {
				t.Fatal("invalid identity produced semantic relation", r)
			}
		}
	}
}

func TestGoManifestOrganizesAndBindsAcrossBatches(t *testing.T) {
	ctx := context.Background()
	docs := []Document{
		{Path: "main.go", Content: []byte("package app\nimport \"example.org/app/lib\"\nfunc Run(){lib.Work()}\n")},
		{Path: "lib/work.go", Content: []byte("package lib\nfunc Work(){}\n")},
		{Path: "go.mod", Content: []byte("module example.org/app\n")},
		{Path: "nested/go.mod", Content: []byte("module example.org/nested\n")},
		{Path: "nested/item.go", Content: []byte("package nested\nfunc Item(){}\n")},
	}
	b, err := NewBuilder("s", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.addDocumentsSync(ctx, docs[:2]...); err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	oldNodes, oldEdges := old.Nodes(), old.Relations()
	if got := query(t, old, `MATCH (:Function {name:'Run'})-[:calls]->(:Function {name:'Work'}) RETURN 1 AS found`, nil); len(got) != 0 {
		t.Fatal("unsupplied module resolved", got)
	}
	if _, err = b.addDocumentsSync(ctx, docs[2:]...); err != nil {
		t.Fatal(err)
	}
	g := b.Result()
	if !reflect.DeepEqual(old.Nodes(), oldNodes) || !reflect.DeepEqual(old.Relations(), oldEdges) {
		t.Fatal("old graph mutated")
	}
	rows := query(t, g, `MATCH (:Document {path:'go.mod'})-[:declares]->(m:Module)-[:contains]->(p:Package) RETURN m.name AS module,p.qualifiedName AS package`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r["module"] != "example.org/app" || strings.Contains(r["package"].(string), "nested") {
			t.Fatal(rows)
		}
	}
	if got := query(t, g, `MATCH (:Function {name:'Run'})-[:calls]->(:Function {name:'Work'}) RETURN 1 AS found`, nil); len(got) != 1 {
		t.Fatal(got)
	}
	if got := query(t, g, `MATCH (:Document {path:'nested/go.mod'})-[:declares]->(:Module {name:'example.org/nested'})-[:contains]->(:Package {qualifiedName:'example.org/nested'}) RETURN 1 AS found`, nil); len(got) != 1 {
		t.Fatal(got)
	}
	if got := query(t, g, `MATCH (:Module) RETURN 1 AS found`, nil); len(got) != 2 {
		t.Fatal("duplicate module identity", got)
	}
	for _, r := range g.Relations() {
		if r.Kind == Declares && r.Source == DocumentID("go.mod") {
			if r.Location.Path != "go.mod" || r.Location.StartByte != 7 || len(r.Evidence) != 1 || r.Evidence[0].Basis != "source_manifest" {
				t.Fatal(r)
			}
		}
	}
	// Submission order and repeat admission must not change facts or identity.
	if _, err = b.addDocumentsSync(ctx, docs...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Nodes(), b.Result().Nodes()) || !reflect.DeepEqual(g.Relations(), b.Result().Relations()) {
		t.Fatal("repeat is not idempotent")
	}
	for i, j := 0, len(docs)-1; i < j; i, j = i+1, j-1 {
		docs[i], docs[j] = docs[j], docs[i]
	}
	reverse, _, err := Build(ctx, "s", docs, Options{BuildConcurrency: 1})
	if err != nil || !reflect.DeepEqual(g.Nodes(), reverse.Nodes()) || !reflect.DeepEqual(g.Relations(), reverse.Relations()) {
		t.Fatalf("order-dependent graph: %v", err)
	}
}

func TestManifestContextConflictAndIsolation(t *testing.T) {
	cache, _ := NewExtractionCache(0, 0)
	e := newTestExtractor(t, cache)
	f := extractTestFacts(t, e, "go.mod", "module example.org/actual\n")
	f.Manifest.Name = "corrupted"
	f.Manifest.NameLocation.StartByte = 999
	f.DocumentKind = GitlinkDocument
	context := ResolutionContext{GoModules: map[string]string{".": "example.org/hint"}}
	g := buildTestFacts(t, "s", Options{ResolutionContext: context}, f, extractTestFacts(t, e, "a.go", "package app\n"))
	if context.GoModules["."] != "example.org/hint" || !hasDiagnostic(g.Report(), "conflicting_module_context") {
		t.Fatal(g.Report(), context)
	}
	d, _ := g.Document("go.mod")
	if d.Manifest.Name != "example.org/actual" || d.Manifest.NameLocation.StartByte != 7 {
		t.Fatal(d)
	}
	d.Manifest.Name = "mutated"
	d.Manifest.NameLocation.StartByte = 100
	again, _ := g.Document("go.mod")
	if again.Manifest.Name != "example.org/actual" || again.Manifest.NameLocation.StartByte != 7 {
		t.Fatal("mutable graph properties", again)
	}
	cached := extractTestFacts(t, e, "go.mod", "module example.org/actual\n")
	if cached.Manifest.Name != "example.org/actual" || cached.Manifest.NameLocation.StartByte != 7 {
		t.Fatal("cache mutated", cached)
	}
	if got := query(t, g, `MATCH (m:Module) RETURN m.name AS name`, nil); len(got) != 1 || got[0]["name"] != "example.org/actual" {
		t.Fatal(got)
	}
}
