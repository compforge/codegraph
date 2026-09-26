package codegraph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestMultilanguageGraph(t *testing.T) {
	for _, tc := range []struct{ path, source, language string }{
		{"app.py", "# +spec=Entry contract\ndef entry():\n    work()\n\ndef work():\n    pass\n", "python"},
		{"app.js", "// +spec=Entry contract\nfunction entry(){ work() }\nfunction work(){}", "javascript"},
		{"app.ts", "// +spec=Entry contract\nfunction entry(): void { work() }\nfunction work(): void {}", "typescript"},
		{"app.tsx", "// +spec=Entry contract\nfunction entry(){ work() }\nfunction work(){}", "tsx"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			g, r, err := Build(context.Background(), "rev", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil || len(r.Diagnostics) != 0 {
				t.Fatal(r, err)
			}
			rows := query(t, g, `MATCH (a:Function {name:'entry'})-[r:calls]->(b:Function {name:'work'}) RETURN a,r,b`, nil)
			if len(rows) != 1 {
				t.Fatal(rows)
			}
			a := rows[0]["a"].(Node)
			edge := rows[0]["r"].(Relation)
			if a.Language != tc.language || edge.Confidence != Exact || len(a.Markers) != 1 || a.Markers[0].Text != "Entry contract" {
				t.Fatal(rows)
			}
			if len(query(t, g, `MATCH (:Symbol) RETURN 1`, nil)) != 0 {
				t.Fatal("generic Symbol kind leaked")
			}
		})
	}
}

func TestRegisteredLanguageOutline(t *testing.T) {
	for _, tc := range []struct {
		path, source, name string
		kind               NodeKind
	}{
		{"Main.java", "class Main { void run() {} }", "Main", Class},
		{"main.rs", "fn run() {}", "run", Function},
		{"main.c", "void run(void) {}", "run", Function},
		{"main.cpp", "void run() {}", "run", Function},
		{"main.rb", "def run\nend\n", "run", Method},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, r, err := Build(context.Background(), "rev", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil || !hasDiagnostic(r, "unsupported_resolution") || (len(r.Diagnostics) == 0) {
				t.Fatal(r, err)
			}
			found := false
			for _, n := range g.Nodes() {
				if n.Name == tc.name && n.Kind == tc.kind {
					found = true
				}
			}
			if !found {
				t.Fatal("missing concrete declaration", g.Nodes(), r)
			}
		})
	}
}

func TestModuleImportExpansion(t *testing.T) {
	for _, tc := range []struct{ entry, target, source, dependency string }{
		{"src/app.ts", "src/lib.ts", "import {work} from './lib'; export function entry() {}", "export function work() {}"},
		{"pkg/app.py", "pkg/lib.py", "from .lib import work\ndef entry():\n    pass\n", "def work():\n    pass\n"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			fs := fstest.MapFS{tc.entry: {Data: []byte(tc.source)}, tc.target: {Data: []byte(tc.dependency)}}
			g, r, err := Build(context.Background(), "rev", documents(fs, tc.entry), Options{})
			if err != nil || !hasDiagnostic(r, "unresolved_import") {
				t.Fatal(r, err)
			}
			r, err = g.addDocumentsSync(context.Background(), documents(fs, tc.target)...)
			if err != nil || len(r.Diagnostics) != 0 {
				t.Fatal(r, err)
			}
			rows := query(t, g, `MATCH (a:File)-[r:imports]->(b:File) RETURN a,r,b`, nil)
			if len(rows) != 1 || rows[0]["b"].(Node).Location.Path != tc.target || rows[0]["r"].(Relation).Confidence != Exact {
				t.Fatal(rows)
			}
			g2, r, err := Build(context.Background(), "rev", documents(fs, tc.entry, tc.target), Options{})
			if err != nil || len(r.Diagnostics) != 0 || !reflect.DeepEqual(g.Nodes(), g2.Nodes()) || !reflect.DeepEqual(g.Relations(), g2.Relations()) {
				t.Fatal(r, err)
			}
		})
	}
}

func TestModuleUncertainty(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.py", "def work():\n    pass\ndef entry(work):\n    work()\n"},
		{"app.js", "function work(){} function entry(work){work()}"},
		{"app.ts", "function work(){} function entry(){const work=()=>{}; work()}"},
		{"app.py", "def work():\n    pass\ndef entry():\n    obj.work()\n"},
		{"app.py", "def work():\n    pass\ndef entry():\n    return lambda: work()\n"},
		{"app.js", "function work(){} function entry(){return () => work()}"},
		{"app.ts", "function work(){} function entry(){return function(){work()}}"},
	} {
		g, r, err := Build(context.Background(), "rev", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
		if err != nil || (len(r.Diagnostics) == 0) || !hasDiagnostic(r, "dynamic_call") {
			t.Fatal(r, err)
		}
		if len(query(t, g, `MATCH ()-[:calls]->() RETURN 1`, nil)) != 0 {
			t.Fatal("shadowed or receiver call became an edge", g.Relations())
		}
	}
}

func TestMixedLanguageIsolationAndRollback(t *testing.T) {
	fs := fstest.MapFS{"a.go": {Data: []byte("package p; func Work(){}; func Entry(){Work()}")}, "a.py": {Data: []byte("def Work():\n    pass\ndef Entry():\n    Work()\n")}}
	g, r, err := Build(context.Background(), "rev", documents(fs, "a.go", "a.py"), Options{})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	for _, row := range query(t, g, `MATCH (a)-[:calls]->(b) RETURN a,b`, nil) {
		if row["a"].(Node).Language != row["b"].(Node).Language {
			t.Fatal("cross-language name binding", row)
		}
	}
	before := g.Nodes()
	fs["a.py"].Data = []byte("def Changed():\n    pass\n")
	if _, err = g.addDocumentsSync(context.Background(), documents(fs, "a.py")...); !errors.Is(err, ErrSnapshotChanged) || !reflect.DeepEqual(before, g.Nodes()) {
		t.Fatal(err)
	}
	g, err = New("rev", Options{MaxNodes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.addDocumentsSync(context.Background(), documents(fs, "a.go", "a.py")...); !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
		t.Fatal("non-Go bypassed atomic budget", err)
	}
}

func TestModuleDeclarationsAndMarkers(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		want         map[string]NodeKind
		marked       string
	}{
		{"app.py", "# +spec=Class contract\nclass Box:\n    # +rule=Method contract\n    def run(self):\n        pass\n", map[string]NodeKind{"Box": Class, "Box.run": Method}, "Box.run"},
		{"app.ts", "// +rule=Export contract\nexport class Box { run() {} }\ninterface Reader {}\ntype Alias = string;\nconst work = () => {};\nenum Mode { On, Off }", map[string]NodeKind{"Box": Class, "Box.run": Method, "Reader": Interface, "Alias": TypeAlias, "work": Variable, "Mode": Enum}, "Box"},
		{"app.js", "// +rule=Export contract\nexport function entry() {}", map[string]NodeKind{"entry": Function}, "entry"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, r, err := Build(context.Background(), "rev", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil || len(r.Diagnostics) != 0 {
				t.Fatal(r, err)
			}
			got := map[string]NodeKind{}
			for _, n := range g.Nodes() {
				if n.Kind != File {
					got[n.QualifiedName] = n.Kind
				}
				if n.QualifiedName == tc.marked && (len(n.Markers) != 1 || n.Markers[0].Kind != Rule) {
					t.Fatal("marker ownership", n)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, tc.want)
			}
			if tc.marked == "Box.run" && len(query(t, g, `MATCH (:Class)-[:contains]->(:Method {name:'run'}) RETURN 1`, nil)) != 1 {
				t.Fatal("method containment lost")
			}
		})
	}
}

func TestImportCandidatesAndScope(t *testing.T) {
	fs := fstest.MapFS{
		"src/app.ts": {Data: []byte("import './lib';")},
		"src/lib.ts": {Data: []byte("export function run(){}")},
		"src/lib.js": {Data: []byte("export function run(){}")},
	}
	g, r, err := Build(context.Background(), "rev", documents(fs, "src/app.ts", "src/lib.ts", "src/lib.js"), Options{})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	rows := query(t, g, `MATCH ()-[r:imports]->() RETURN r`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Candidate {
			t.Fatal(row)
		}
	}
	g, r, err = Build(context.Background(), "rev", documents(fs, "src/app.ts", "src/lib.ts", "src/lib.js"), Options{Scope: []string{"src/app.ts"}})
	if err != nil || (len(r.Diagnostics) == 0) || len(g.Nodes()) != 1 || !hasDiagnostic(r, "out_of_scope") {
		t.Fatal(r, err)
	}
}

func TestUnsupportedAndMalformedLanguages(t *testing.T) {
	for _, tc := range []struct{ path, source, code string }{
		{"data.json", "{\"x\":1}", "outline_incomplete"},
		{"bad.py", "def :\n", "parse_error"},
		{"bad.ts", "function {", "parse_error"},
		{"unknown.cg-unrecognized", "text", "unsupported_language"},
		{"app.js", "import(target)", "dynamic_import"},
	} {
		_, r, err := Build(context.Background(), "rev", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
		if err != nil || (len(r.Diagnostics) == 0) || !hasDiagnostic(r, tc.code) {
			t.Fatal(tc.path, r, err)
		}
	}
}

func TestLanguageDiscoveryAndCapabilities(t *testing.T) {
	for path, want := range map[string]string{"a.pyi": "python", "a.mts": "typescript", "a.cts": "typescript", "a.jsx": "javascript", "a.mjs": "javascript", "a.cjs": "javascript", "a.rs": "rust", "a.cpp": "cpp"} {
		if got := Language(path); got != want {
			t.Fatalf("%s: %s != %s", path, got, want)
		}
	}
	if len(Capabilities("missing-language")) != 0 {
		t.Fatal("unknown language capability invented")
	}
	for _, cap := range Capabilities() {
		relations := []RelationKind{Contains, Imports, Calls, References, Extends}
		if cap.Language == "go" || cap.Language == "typescript" || cap.Language == "tsx" {
			relations = append(relations, Implements)
		}
		if len(cap.Declarations) == 0 || !reflect.DeepEqual(cap.Relations, relations) || len(cap.Markers) != 5 {
			t.Fatal(cap)
		}
	}
	cap := Capabilities("rust")
	if len(cap) != 1 || len(cap[0].Declarations) == 0 || !reflect.DeepEqual(cap[0].Relations, []RelationKind{Contains}) {
		t.Fatal(cap)
	}
}

func TestMultilanguageExpansionBudget(t *testing.T) {
	fs := fstest.MapFS{"a.js": {Data: []byte("import './b.js';")}, "b.js": {Data: []byte("import './c.js';")}, "c.js": {Data: []byte("function work(){}")}}
	for _, opts := range []Options{{MaxFiles: 2}, {MaxRelations: 1}} {
		g, err := New("rev", opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = g.addDocumentsSync(context.Background(), documents(fs, "a.js", "b.js", "c.js")...); !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
			t.Fatal("import budget did not roll back", err)
		}
	}
}
