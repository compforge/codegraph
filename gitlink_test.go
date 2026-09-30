package codegraph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const gitlinkCommit = "0123456789abcdef0123456789abcdef01234567"

// +case=`A supplied gitlink is one versioned Document, regardless of filename or checkout availability`
func TestGitlinkDocumentIdentity(t *testing.T) {
	ctx := context.Background()
	g, err := New("parent", Options{})
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
	if err != nil || len(report.Diagnostics) != 0 || len(g.Nodes()) != 1 || len(g.Relations()) != 0 {
		t.Fatal(report, err, g.Nodes())
	}
	node, ok := g.Node(document.ID())
	if !ok || node.Kind != DocumentKind || node.Gitlink != gitlinkCommit || node.Location.Path != document.Path || node.Location.EndByte != 0 {
		t.Fatal(node)
	}
	rows := query(t, g, `MATCH (n:Document {gitlink:$commit}) RETURN n,n.gitlink AS revision`, map[string]any{"commit": gitlinkCommit})
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
	still, _ := g.Node(document.ID())
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
				if edge.Target == gitlink.ID() {
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
	edges := g.RelationsTo(DocumentID("sdk"), Imports)
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
	g, _, err := Build(ctx, "parent", []Document{source}, Options{})
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
	if err != nil || !reflect.DeepEqual(g.Nodes(), all.Nodes()) || !reflect.DeepEqual(g.Relations(), all.Relations()) {
		t.Fatal("batch order changed graph", err)
	}
	for _, options := range []Options{{MaxDocumentBytes: 39}, {MaxSourceBytes: 79}, {MaxDocuments: 1}, {MaxNodes: 1}} {
		g, err := New("limited", options)
		if err != nil {
			t.Fatal(err)
		}
		err = g.AddDocuments(ctx, gitlink, Document{Path: "other", Gitlink: gitlinkCommit})
		if err == nil {
			_, err = g.Wait(ctx)
		}
		if !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
			t.Fatal("gitlink bypassed budget or partial publication", options, err, g.Nodes())
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
		g, _ := New("invalid", Options{})
		if _, err := g.Extract(ctx, bad); err == nil {
			t.Fatal("invalid extraction accepted", bad)
		}
		if err := g.AddDocuments(ctx, Document{Path: "ok.ts", Content: []byte("function ok(){}")}, bad); err == nil || len(g.legacy.documentTasks) != 0 {
			t.Fatal("invalid batch partially admitted", err)
		}
	}
	link := Document{Path: "sdk", Gitlink: gitlinkCommit}
	child := Document{Path: "sdk/api.ts", Content: []byte("export function work(){}")}
	for _, docs := range [][]Document{{link, child}, {child, link}, {link, {Path: "sdk/nested", Gitlink: gitlinkCommit}}} {
		g, _ := New("overlap", Options{})
		if err := g.AddDocuments(ctx, docs...); err == nil || len(g.legacy.documentTasks) != 0 {
			t.Fatal("mixed snapshots admitted", err)
		}
	}
	for _, docs := range [][]Document{{link, child}, {child, link}} {
		g, _ := New("batches", Options{})
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
