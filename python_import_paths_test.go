package codegraph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// +case:id=python-package-layout,expect=`Absolute imports use bounded layout candidates and never claim runtime search-path certainty`
func TestPythonAbsoluteImportsBelowSnapshotRoot(t *testing.T) {
	for _, prefix := range []string{"", "src/", "sdks/python/source/"} {
		t.Run(prefix, func(t *testing.T) {
			g, _, err := Build(context.Background(), "layout", []Document{
				{Path: prefix + "pkg/app.py", Content: []byte("from pkg.lib import work as run\nimport pkg.lib as api\ndef entry():\n    run()\n    api.work()\ndef shadow(run):\n    run()\n")},
				{Path: prefix + "pkg/lib.py", Content: []byte("def work(): pass\n")},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(target:Function {name:'work'}) RETURN r,target`, nil)
			if len(rows) != 2 {
				t.Fatalf("calls=%v diagnostics=%v", rows, g.Report().Diagnostics)
			}
			for _, row := range rows {
				if row["r"].(Relation).Confidence != Candidate || row["target"].(Node).Location.Path != prefix+"pkg/lib.py" {
					t.Fatal(row)
				}
			}
			if rows := query(t, g, `MATCH (:Function {name:'shadow'})-[:calls]->(:Function {name:'work'}) RETURN 1`, nil); len(rows) != 0 {
				t.Fatal(rows)
			}
			refs := query(t, g, `MATCH (:Function {name:'entry'})-[r:references]->(:Function {name:'work'}) RETURN r`, nil)
			if len(refs) != 2 {
				t.Fatal(refs)
			}
			for _, row := range refs {
				if row["r"].(Relation).Confidence != Candidate {
					t.Fatal(row)
				}
			}
		})
	}
}

func TestPythonPackageLayoutReExportAndReload(t *testing.T) {
	ctx := context.Background()
	g, report, err := Build(ctx, "reload", []Document{{Path: "sdk/pkg/app.py", Content: []byte("from pkg import run\ndef entry(): return run()\n")}}, Options{})
	if err != nil || !hasDiagnostic(report, "unresolved_import") {
		t.Fatal(report, err)
	}
	if err := g.AddDocuments(ctx,
		Document{Path: "sdk/pkg/__init__.py", Content: []byte("from .lib import work as run\n")},
		Document{Path: "sdk/pkg/lib.py", Content: []byte("def work(): pass\n")},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(target:Function {name:'work'}) RETURN r,target`, nil)
	if len(rows) != 1 || rows[0]["r"].(Relation).Confidence != Candidate || rows[0]["target"].(Node).Location.Path != "sdk/pkg/lib.py" {
		t.Fatal(rows)
	}
}

func TestPythonPackageLayoutAmbiguityAndBoundaries(t *testing.T) {
	g, _, err := Build(context.Background(), "ambiguous", []Document{
		{Path: "src/pkg/app.py", Content: []byte("from pkg.lib import work\nfrom external.lib import missing\ndef entry():\n    work()\n    missing()\n")},
		{Path: "pkg/lib.py", Content: []byte("def work(): pass\n")},
		{Path: "src/pkg/lib.py", Content: []byte("def work(): pass\n")},
		{Path: "elsewhere/pkg/lib.py", Content: []byte("def work(): pass\n")},
		{Path: "src/external/lib.py", Content: []byte("def missing(): pass\n")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(target:Function {name:'work'}) RETURN r,target`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Candidate || strings.HasPrefix(row["target"].(Node).Location.Path, "elsewhere/") {
			t.Fatal(row)
		}
	}
	if rows := query(t, g, `MATCH (:Function {name:'entry'})-[:calls]->(:Function {name:'missing'}) RETURN 1`, nil); len(rows) != 0 {
		t.Fatal("unrelated source roots must not be guessed", rows)
	}
}

func TestPythonPackageLayoutStubsAndRelativeImports(t *testing.T) {
	for _, ext := range []string{".py", ".pyi"} {
		t.Run(ext, func(t *testing.T) {
			g, _, err := Build(context.Background(), "forms", []Document{
				{Path: "sdk/pkg/app.py", Content: []byte("from .lib import work as relative\nfrom pkg.lib import work as absolute\ndef entry():\n    relative()\n    absolute()\n")},
				{Path: "sdk/pkg/lib" + ext, Content: []byte("def work(): ...\n")},
			}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (:Function {name:'entry'})-[r:calls]->(:Function {name:'work'}) RETURN r`, nil)
			if len(rows) != 2 {
				t.Fatal(rows)
			}
			counts := map[Confidence]int{}
			for _, row := range rows {
				counts[row["r"].(Relation).Confidence]++
			}
			if counts[Exact] != 1 || counts[Candidate] != 1 {
				t.Fatal(counts)
			}
		})
	}
}

func TestPythonPackageLayoutBudgetIsAtomic(t *testing.T) {
	ctx := context.Background()
	g, err := New("budget", Options{MaxRelations: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.AddDocuments(ctx,
		Document{Path: "src/pkg/app.py", Content: []byte("from pkg.lib import work\ndef entry(): return work()\n")},
		Document{Path: "src/pkg/lib.py", Content: []byte("def work(): pass\n")},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(ctx); !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 || len(g.Relations()) != 0 {
		t.Fatal(err, g.Nodes(), g.Relations())
	}
}
