package codegraph

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDirectoryPathsAndIndependentTags(t *testing.T) {
	ctx := context.Background()
	rules := []TagRule{
		{Name: "test", Pattern: `^fixtures$`},
		{Name: GeneratedTag, Pattern: `\.pb\.go$`},
		{Name: GeneratedTag, Pattern: `^fixtures/`},
		{Name: "custom", Pattern: `^fixtures/a\.pb\.go$`},
	}
	b, err := NewBuilder("s", Options{TagRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	rules[0].Name, rules[1].Pattern = "mutated", "["
	_, err = b.addDocumentsSync(ctx,
		Document{Path: "fixtures/a.pb.go", Content: []byte("package p\nfunc F(){}\n")},
		Document{Path: "fixtures/other.txt", Content: []byte("unknown")},
	)
	if err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	dir, ok := old.Directory("fixtures")
	if !ok || dir.Path != "fixtures" || dir.Location != nil || !reflect.DeepEqual(dir.Tags, []Tag{"test"}) {
		t.Fatal(dir)
	}
	if _, ok := old.Directory("."); !ok {
		t.Fatal("missing root")
	}
	if _, ok := old.Directory("absent"); ok {
		t.Fatal("invented directory")
	}
	d, _ := old.Document("fixtures/a.pb.go")
	if d.Path != "fixtures/a.pb.go" || !reflect.DeepEqual(d.Tags, []Tag{"custom", GeneratedTag}) {
		t.Fatal(d)
	}
	other, _ := old.Document("fixtures/other.txt")
	if slices.Contains(other.Tags, "test") {
		t.Fatal("inherited directory tag", other)
	}
	for _, n := range old.Nodes() {
		if n.Kind != DocumentNodeKind && n.Kind != DirectoryNodeKind && len(n.Tags) > 0 {
			t.Fatal("tagged symbol", n)
		}
	}
	rows := query(t, old, `MATCH (d:Document)-[:in_directory]->(p:Directory) WHERE 'custom' IN d.tags RETURN d, p.path AS parent`, nil)
	if len(rows) != 1 || rows[0]["parent"] != "fixtures" || !reflect.DeepEqual(rows[0]["d"], d.Node) {
		t.Fatal(rows)
	}
	rows = query(t, old, `MATCH (:Directory {path:'fixtures'})-[:in_directory]->(p:Directory) RETURN p.path AS parent`, nil)
	if len(rows) != 1 || rows[0]["parent"] != "." {
		t.Fatal(rows)
	}
	matches, err := old.NamespaceAncestors(ctx, d.ID, NamespaceOptions{})
	if err != nil || len(matches) != 1 || matches[0].Node.Kind != Package {
		t.Fatal(matches, err)
	}
	matches, err = old.NamespaceAncestors(ctx, dir.ID, NamespaceOptions{})
	if err != nil || len(matches) != 0 {
		t.Fatal("directory became namespace", matches, err)
	}
	common, err := old.CommonNamespaces(ctx, []string{d.ID, other.ID}, NamespaceOptions{})
	if err != nil || len(common) != 0 {
		t.Fatal("directory grouped unrelated material", common, err)
	}
	d.Tags[0], dir.Tags[0] = "changed", "changed"
	again, _ := old.Document(d.Path)
	againDir, _ := old.Directory(dir.Path)
	if again.Tags[0] != "custom" || againDir.Tags[0] != "test" {
		t.Fatal("mutable graph tags")
	}
	beforeNodes, beforeEdges := old.Nodes(), old.Relations()
	if _, err := b.addDocumentsSync(ctx, Document{Path: "fixtures/sub/new.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Result().Directory("fixtures/sub"); !ok {
		t.Fatal("missing new ancestor")
	}
	if !reflect.DeepEqual(old.Nodes(), beforeNodes) || !reflect.DeepEqual(old.Relations(), beforeEdges) {
		t.Fatal("old graph changed")
	}
	if _, ok := old.Directory("fixtures/sub"); ok {
		t.Fatal("new directory leaked into old graph")
	}
}

func TestBuiltinTagsAndReplacement(t *testing.T) {
	docs := []Document{
		{Path: "vendor/example/go.mod", Content: []byte("module example.org/lib\n")},
		{Path: "kitex_gen/service.pb.go", Content: []byte("package service\n")},
		{Path: "dist/app.min.js"}, {Path: "fixtures/input.txt"},
		{Path: "x.generated.go", Content: []byte("package x\n")},
		{Path: ".cache/data.txt"}, {Path: "node_modules/pkg/a.min.css"},
		{Path: "__snapshots__/result.txt"}, {Path: ".next/server.js"},
	}
	g, _, err := Build(context.Background(), "s", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Tag{
		{DependencyTag, ManifestTag}, {GeneratedTag}, {BuildOutputTag, MinifiedTag},
		{TestFixtureTag}, {GeneratedTag}, {CacheTag}, {DependencyTag, MinifiedTag},
		{TestFixtureTag}, {BuildOutputTag},
	}
	for i, doc := range docs {
		n, _ := g.Document(doc.Path)
		if !reflect.DeepEqual(n.Tags, want[i]) {
			t.Fatalf("%s: %v", doc.Path, n.Tags)
		}
	}
	dir, _ := g.Directory("vendor")
	if !reflect.DeepEqual(dir.Tags, []Tag{DependencyTag}) {
		t.Fatal(dir)
	}
	rules := BuiltinTagRules()
	rules[0].Name = "corrupted"
	if BuiltinTagRules()[0].Name != ManifestTag {
		t.Fatal("shared builtin rules")
	}
	for _, rules := range [][]TagRule{{}, {{Name: "custom", Pattern: `go\.mod$`}}} {
		custom, _, err := Build(context.Background(), "s", docs[:1], Options{TagRules: rules})
		if err != nil {
			t.Fatal(err)
		}
		n, _ := custom.Document(docs[0].Path)
		if slices.Contains(n.Tags, ManifestTag) || n.Manifest == nil || n.Manifest.Name != "example.org/lib" {
			t.Fatal("tags controlled parsing", n)
		}
		if len(n.Tags) != len(rules) {
			t.Fatal(n.Tags)
		}
	}
	arbitrary, _, err := Build(context.Background(), "s", []Document{{Path: "notes.txt"}}, Options{TagRules: []TagRule{{Name: ManifestTag, Pattern: `.*`}}})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := arbitrary.Document("notes.txt")
	if !slices.Contains(n.Tags, ManifestTag) || n.Manifest != nil {
		t.Fatal(n)
	}
}

func TestTagRuleValidation(t *testing.T) {
	for _, rule := range []TagRule{{Name: "test", Pattern: "["}, {Name: " ", Pattern: ".*"}} {
		if _, err := NewBuilder("s", Options{TagRules: []TagRule{rule}}); err == nil {
			t.Fatal("invalid rule accepted", rule)
		}
	}
}

func TestDirectoryBudgetRollback(t *testing.T) {
	ctx := context.Background()
	for _, opts := range []Options{{MaxNodes: 2}, {MaxRelations: 1}, {MaxEvidence: 1}} {
		b, err := NewBuilder("s", opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.addDocumentsSync(ctx, Document{Path: "seed.unknown"}); err != nil {
			t.Fatal(err)
		}
		old := b.Result()
		if _, err = b.addDocumentsSync(ctx, Document{Path: "a/b/new.unknown"}); !errors.Is(err, ErrBuildBudget) {
			t.Fatal(err)
		}
		if b.Result() != old {
			t.Fatal("failed structure build published")
		}
	}
}

func TestDirectoryForGitlinkAndDeterministicAdmission(t *testing.T) {
	docs := []Document{{Path: "a/sdk", Gitlink: strings.Repeat("a", 40)}, {Path: "a/b/data.txt"}, {Path: "a/other.txt"}}
	ctx := context.Background()
	first, _, err := Build(ctx, "s", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first.Directory("a/sdk"); ok {
		t.Fatal("opaque gitlink became a directory")
	}
	if _, ok := first.Directory("a"); !ok {
		t.Fatal("missing gitlink parent")
	}
	slices.Reverse(docs)
	e := newTestExtractor(t, nil)
	b, _ := NewBuilder("s", Options{})
	for _, doc := range docs {
		facts, err := e.Extract(ctx, doc)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.Add(facts); err != nil {
			t.Fatal(err)
		}
	}
	second, _, err := b.Build(ctx)
	if err != nil || !reflect.DeepEqual(first.Nodes(), second.Nodes()) || !reflect.DeepEqual(first.Relations(), second.Relations()) {
		t.Fatal("admission order or API changed graph", err)
	}
}
