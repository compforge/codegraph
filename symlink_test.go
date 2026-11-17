package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// +case:id=symlink_material,desc=`A file alias is a distinct, unparsed Document`,expect=`Target spelling, tags and links_to agree across graph views and cache versions`
func TestSymlinkMaterial(t *testing.T) {
	ctx := context.Background()
	cache, _ := NewExtractionCache(10, 4096)
	extractor, _ := NewExtractor(ExtractionOptions{Cache: cache})
	docs := []Document{
		{Path: "alias.go", Content: []byte("package app\nfunc Original(){}")},
		{Path: "alias.go", Gitlink: gitlinkCommit},
		{Path: "alias.go", Symlink: gitlinkCommit},
		{Path: "alias.go", Symlink: "target.go"},
	}
	var link Facts
	for _, doc := range docs {
		f, err := extractor.Extract(ctx, doc)
		if err != nil {
			t.Fatal(err)
		}
		if f.Symlink != doc.Symlink || f.Gitlink != doc.Gitlink {
			t.Fatal("cache confused materials", f)
		}
		if doc.isLink() && (f.Language != "" || len(f.Declarations) != 0 || len(f.Issues) != 0) {
			t.Fatal("link parsed as source", f)
		}
		link = f
	}
	if len(cache.entries) != 4 {
		t.Fatal("cache identity collision", len(cache.entries))
	}
	target, err := extractor.Extract(ctx, Document{Path: "target.go", Content: []byte("package app\nfunc Target(){}")})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewBuilder("snapshot", Options{})
	if err := b.Add(link, target); err != nil {
		t.Fatal(err)
	}
	g, report, err := b.Build(ctx)
	if err != nil || len(report.Diagnostics) != 0 {
		t.Fatal(report, err)
	}
	n, ok := g.Document("alias.go")
	if !ok || n.Symlink != "target.go" || n.Language != "" || !reflect.DeepEqual(n.Tags, []Tag{SymlinkTag}) {
		t.Fatal(n)
	}
	rows := query(t, g, `MATCH (a:Document)-[:links_to]->(b:Document) RETURN a.symlink AS target, b.path AS path`, nil)
	if len(rows) != 1 || rows[0]["target"] != "target.go" || rows[0]["path"] != "target.go" {
		t.Fatal(rows)
	}
	encoded, _ := json.Marshal(n)
	if !strings.Contains(string(encoded), `"symlink":"target.go"`) {
		t.Fatal(string(encoded))
	}
	changed, _ := extractor.Extract(ctx, Document{Path: "alias.go", Symlink: "other.go"})
	if err := b.Add(changed); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatal(err)
	}
	n.Symlink = "changed"
	still, _ := g.Document("alias.go")
	if still.Symlink != "target.go" {
		t.Fatal("graph changed")
	}
}

// +case:id=symlink_snapshot_relations,desc=`Resolve immediate references within supplied snapshot entries`,expect=`Chains and cycles remain explicit; invalid traversal keeps raw target and diagnostics`
func TestSymlinkTargets(t *testing.T) {
	base := []Document{{Path: "dir/file.go", Content: []byte("package app")}, {Path: "module", Gitlink: gitlinkCommit}, {Path: "folder", Symlink: "dir"}, {Path: "chain", Symlink: "dir/file.go"}}
	for _, tc := range []struct{ name, target, id string }{
		{"file", "dir/file.go", DocumentID("dir/file.go")},
		{"chain", "chain", DocumentID("chain")},
		{"directory", "dir", DirectoryID("dir")},
		{"directory slash", "dir/", DirectoryID("dir")},
		{"parent", "dir/../dir/file.go", DocumentID("dir/file.go")},
		{"root", ".", DirectoryID(".")},
		{"gitlink", "module", DocumentID("module")},
		{"self", "alias", DocumentID("alias")},
		{"missing", "missing", ""},
		{"absolute", "/dir/file.go", ""},
		{"escape", "../dir/file.go", ""},
		{"missing before parent", "missing/../dir/file.go", ""},
		{"file before parent", "dir/file.go/../file.go", ""},
		{"file slash", "dir/file.go/", ""},
		{"directory alias", "folder/file.go", ""},
		{"alias before parent", "folder/../dir/file.go", ""},
		{"gitlink child", "module/file.go", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs := append(slices.Clone(base), Document{Path: "alias", Symlink: tc.target})
			g, report, err := Build(context.Background(), "snapshot", docs, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rels := g.RelationsFrom(DocumentID("alias"), LinksTo)
			if tc.id == "" {
				if len(rels) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "unresolved_symlink" {
					t.Fatal(rels, report)
				}
			} else if len(rels) != 1 || rels[0].Target != tc.id || rels[0].Confidence != Exact || len(report.Diagnostics) != 0 {
				t.Fatal(rels, report)
			}
		})
	}
	g, report, err := Build(context.Background(), "cycle", []Document{{Path: "a", Symlink: "b"}, {Path: "b", Symlink: "a"}}, Options{})
	if err != nil || len(report.Diagnostics) != 0 || len(g.RelationsFrom(DocumentID("a"), LinksTo)) != 1 || len(g.RelationsFrom(DocumentID("b"), LinksTo)) != 1 {
		t.Fatal(report, err)
	}
}

func TestSymlinkBatchesAndAdmission(t *testing.T) {
	ctx := context.Background()
	link := Document{Path: "nested/alias.go", Symlink: "../target.go"}
	target := Document{Path: "target.go", Content: []byte("package app")}
	b, _ := NewBuilder("snapshot", Options{Admission: &AdmissionPolicy{ParseTags: []Tag{SourceTag}}})
	if err := b.AddDocuments(ctx, link); err != nil {
		t.Fatal(err)
	}
	before, err := b.Wait(ctx)
	if err != nil || len(before.Diagnostics) != 1 {
		t.Fatal(before, err)
	}
	old := b.Result()
	if err := b.AddDocuments(ctx, target); err != nil {
		t.Fatal(err)
	}
	after, err := b.Wait(ctx)
	if err != nil || len(after.Diagnostics) != 0 || len(b.Result().RelationsFrom(link.ID(), LinksTo)) != 1 {
		t.Fatal(after, err)
	}
	if len(old.RelationsFrom(link.ID(), LinksTo)) != 0 || len(old.Report().Diagnostics) != 1 {
		t.Fatal("old graph changed")
	}
	for _, changed := range []Document{{Path: link.Path}, {Path: link.Path, Symlink: "other"}, {Path: link.Path, Gitlink: gitlinkCommit}} {
		if err := b.AddDocuments(ctx, changed); !errors.Is(err, ErrSnapshotChanged) {
			t.Fatal(err)
		}
	}
	for _, policy := range []*AdmissionPolicy{
		{ExcludedTags: []Tag{SymlinkTag}},
		{NodeKinds: []NodeKind{DirectoryNodeKind}},
	} {
		g, _, err := Build(ctx, "admission", []Document{link, target}, Options{Admission: policy})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := g.Document(link.Path); ok {
			t.Fatal("link not excluded")
		}
		for _, r := range g.Relations() {
			if r.Kind == LinksTo {
				t.Fatal("dangling link relation", r)
			}
		}
	}
}

func TestSymlinkInvalidBoundariesAndBudgets(t *testing.T) {
	ctx := context.Background()
	extractor, _ := NewExtractor(ExtractionOptions{})
	for _, doc := range []Document{{Path: ".", Symlink: "file"}, {Path: "alias", Symlink: "a\x00b"}, {Path: "alias", Symlink: "file", Content: []byte("x")}, {Path: "alias", Symlink: "file", Gitlink: gitlinkCommit}} {
		if _, err := extractor.Extract(ctx, doc); err == nil {
			t.Fatal("invalid link accepted", doc)
		}
		b, _ := NewBuilder("invalid", Options{})
		if err := b.AddDocuments(ctx, Document{Path: "ok.go"}, doc); err == nil {
			t.Fatal("invalid batch accepted")
		}
		if _, err := b.GetDocument(DocumentID("ok.go")); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatal("partial batch", err)
		}
	}
	link := Document{Path: "alias", Symlink: "target"}
	child := Document{Path: "alias/child.go", Content: []byte("package app")}
	for _, docs := range [][]Document{{link, child}, {child, link}} {
		b, _ := NewBuilder("batch", Options{})
		if err := b.AddDocuments(ctx, docs...); err == nil {
			t.Fatal("boundary accepted")
		}
		b, _ = NewBuilder("facts", Options{})
		for i, doc := range docs {
			f, err := extractor.Extract(ctx, doc)
			if err != nil {
				t.Fatal(err)
			}
			err = b.Add(f)
			if (i == 0 && err != nil) || (i == 1 && err == nil) {
				t.Fatal("fact boundary", i, err)
			}
		}
		b, _ = NewBuilder("async", Options{})
		if err := b.AddDocuments(ctx, docs[0]); err != nil {
			t.Fatal(err)
		}
		if err := b.AddDocuments(ctx, docs[1]); err == nil {
			t.Fatal("inflight boundary accepted")
		}
		if _, err := b.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.AddDocuments(ctx, docs[1]); err == nil {
			t.Fatal("published boundary accepted")
		}
	}
	docs := []Document{link, {Path: "target", Symlink: "alias"}}
	for _, opts := range []Options{{MaxDocumentBytes: 5}, {MaxSourceBytes: 10}, {MaxRelations: 2}, {MaxEvidence: 2}} {
		b, _ := NewBuilder("budget", opts)
		err := b.AddDocuments(ctx, docs...)
		if err == nil {
			_, err = b.Wait(ctx)
		}
		if !errors.Is(err, ErrBuildBudget) || len(b.Result().Nodes()) != 0 {
			t.Fatal("budget bypass or partial publication", opts, err)
		}
	}
	cache, _ := NewExtractionCache(10, 5)
	e, _ := NewExtractor(ExtractionOptions{Cache: cache})
	if _, err := e.Extract(ctx, link); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != 0 {
		t.Fatal("cache ignored symlink bytes")
	}
}

func TestLinkTags(t *testing.T) {
	for _, rules := range [][]TagRule{nil, {}, {{Name: SymlinkTag, Pattern: ".*"}, {Name: "custom", Pattern: ".*"}}} {
		matcher, err := NewTagMatcher(rules)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range []Document{{Path: "alias.go", Symlink: "target.go"}, {Path: "alias.go", Gitlink: gitlinkCommit}} {
			tags := matcher.DocumentTags(doc)
			want := SymlinkTag
			if doc.Gitlink != "" {
				want = GitlinkTag
			}
			if !slices.Contains(tags, want) || slices.Contains(tags, SourceTag) || slices.Contains(tags, UnknownTag) {
				t.Fatal(tags)
			}
			if !slices.IsSorted(tags) || len(slices.Compact(slices.Clone(tags))) != len(tags) {
				t.Fatal(tags)
			}
		}
	}
	matcher, _ := NewTagMatcher(nil)
	if got := matcher.DocumentTags(Document{Path: "CLAUDE.md", Symlink: "AGENTS.md"}); !reflect.DeepEqual(got, []Tag{SymlinkTag, ToolingTag}) {
		t.Fatal(got)
	}
}
