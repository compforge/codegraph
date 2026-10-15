package codegraph_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	cg "github.com/compforge/codegraph"
)

func TestTagMatcherRulesAndGraphParity(t *testing.T) {
	for _, tt := range []struct {
		name  string
		rules []cg.TagRule
		want  []cg.Tag
	}{
		{"builtin", nil, []cg.Tag{cg.DependencyTag, cg.ManifestTag}},
		{"disabled", []cg.TagRule{}, nil},
		{"custom", []cg.TagRule{
			{Name: "z", Pattern: `go\.mod$`},
			{Name: "a", Pattern: `vendor`},
			{Name: "z", Pattern: `vendor`},
			{Name: "directory", Pattern: `^vendor$`},
		}, []cg.Tag{"a", "z"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := cg.NewTagMatcher(tt.rules)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Match("vendor/lib/go.mod"); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tags = %v, want %v", got, tt.want)
			}
			g, _, err := cg.Build(context.Background(), "s", []cg.Document{
				{Path: "vendor/lib/go.mod", Content: []byte("module example.org/lib\n")},
				{Path: "fixtures/input.txt"},
				{Path: "dist/app.min.js"},
			}, cg.Options{TagRules: tt.rules})
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range g.Nodes() {
				if node.Kind == cg.DocumentNodeKind || node.Kind == cg.DirectoryNodeKind {
					got := m.Match(node.Path)
					if node.Kind == cg.DocumentNodeKind {
						got = m.DocumentTags(cg.Document{Path: node.Path, Gitlink: node.Gitlink})
					}
					if !reflect.DeepEqual(got, node.Tags) {
						t.Fatalf("%s: matcher = %v, graph = %v", node.Path, got, node.Tags)
					}
				}
			}
		})
	}
	var zero cg.TagMatcher
	if got := zero.Match("go.mod"); got != nil {
		t.Fatal(got)
	}
}

func TestTagMatcherValidation(t *testing.T) {
	for _, rule := range []cg.TagRule{{Name: "test", Pattern: "["}, {Name: " \t", Pattern: ".*"}} {
		m, err := cg.NewTagMatcher([]cg.TagRule{rule})
		if m != nil || err == nil || !strings.Contains(err.Error(), "tag rule 0") {
			t.Fatalf("matcher = %v, error = %v", m, err)
		}
	}
}

func TestTagMatcherConcurrentReuse(t *testing.T) {
	rules := []cg.TagRule{{Name: "custom", Pattern: `^fixtures$`}}
	m, err := cg.NewTagMatcher(rules)
	if err != nil {
		t.Fatal(err)
	}
	rules[0] = cg.TagRule{Name: "changed", Pattern: "["}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				got := m.Match("fixtures")
				if !reflect.DeepEqual(got, []cg.Tag{"custom"}) {
					t.Errorf("tags = %v", got)
					return
				}
				got[0] = "changed"
				if got := m.Match("fixtures/input.txt"); got != nil {
					t.Errorf("inherited tags: %v", got)
				}
			}
		}()
	}
	wg.Wait()
}

func ExampleNewTagMatcher() {
	matcher, err := cg.NewTagMatcher(nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(matcher.Match("vendor/lib/go.mod"))
	// Output: [dependency manifest]
}

// +case:id=material_tags,desc=`Format and path classification share Tags while gitlink stays opaque`,expect=`Graph kinds remain separate from material tags`
func TestDocumentMaterialTags(t *testing.T) {
	m, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		document cg.Document
		want     []cg.Tag
	}{
		{cg.Document{Path: "main.go"}, []cg.Tag{cg.SourceTag}},
		{cg.Document{Path: "main.pb.go"}, []cg.Tag{cg.GeneratedTag, cg.SourceTag}},
		{cg.Document{Path: "README.md"}, []cg.Tag{cg.DocumentationTag}},
		{cg.Document{Path: "settings.json"}, []cg.Tag{cg.ResourceTag}},
		{cg.Document{Path: "logo.png"}, []cg.Tag{cg.ResourceTag}},
		{cg.Document{Path: "unknown.xyz"}, nil},
		{cg.Document{Path: "submodule.go", Gitlink: strings.Repeat("a", 40)}, nil},
	} {
		if got := m.DocumentTags(tc.document); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.document.Path, got, tc.want)
		}
	}
	custom, err := cg.NewTagMatcher([]cg.TagRule{{Name: cg.ToolingTag, Pattern: `^workspace$`}, {Name: cg.SourceTag, Pattern: `\.go$`}})
	if err != nil {
		t.Fatal(err)
	}
	doc := cg.Document{Path: "workspace/nested/main.go"}
	if got := custom.ExcludedBy(doc, []cg.Tag{cg.ToolingTag}); got != cg.ToolingTag {
		t.Fatal(got)
	}
	if got := custom.DocumentTags(doc); !reflect.DeepEqual(got, []cg.Tag{cg.SourceTag}) {
		t.Fatal("duplicate or inherited tags", got)
	}
	if got := custom.ExcludedBy(doc, []cg.Tag{cg.SourceTag}); got != cg.SourceTag {
		t.Fatal("material tag not used for exclusion", got)
	}
	disabled, _ := cg.NewTagMatcher([]cg.TagRule{})
	if got := disabled.DocumentTags(doc); !slices.Contains(got, cg.SourceTag) {
		t.Fatal("format classification disabled with path rules", got)
	}
	for _, name := range []string{".idea", ".vscode", ".codex", ".claude", ".devloop"} {
		if got := m.ExcludedBy(cg.Document{Path: "nested/" + name + "/source.go"}, []cg.Tag{cg.ToolingTag}); got != cg.ToolingTag {
			t.Fatal(name, got)
		}
	}
	if got := m.ExcludedBy(cg.Document{Path: ".idea-notes/source.go"}, []cg.Tag{cg.ToolingTag}); got != "" {
		t.Fatal(got)
	}
}

// +case:id=repository_material_tags,desc=`Repository configuration and agent instructions have replaceable path tags`,expect=`Agent instructions retain documentation and tooling tags at any depth`
func TestRepositoryMaterialTags(t *testing.T) {
	builtin, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := cg.NewTagMatcher([]cg.TagRule{})
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "component/"} {
		for _, name := range []string{".dockerignore", ".gitignore", ".ignore", ".gitattributes", ".editorconfig", ".env", ".env.example", ".env.local"} {
			doc := cg.Document{Path: prefix + name}
			want := []cg.Tag{cg.ConfigTag}
			if name == ".editorconfig" {
				want = append(want, cg.ResourceTag)
			}
			if got := builtin.DocumentTags(doc); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: tags = %v", doc.Path, got)
			}
			if got := disabled.DocumentTags(doc); slices.Contains(got, cg.ConfigTag) {
				t.Errorf("disabled config rule: %s: %v", doc.Path, got)
			}
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
			doc := cg.Document{Path: prefix + name}
			if got := builtin.DocumentTags(doc); !reflect.DeepEqual(got, []cg.Tag{cg.DocumentationTag, cg.ToolingTag}) {
				t.Errorf("%s: tags = %v", doc.Path, got)
			}
			if got := disabled.DocumentTags(doc); !reflect.DeepEqual(got, []cg.Tag{cg.DocumentationTag}) {
				t.Errorf("disabled tooling rule lost format: %s: %v", doc.Path, got)
			}
		}
	}
	for _, name := range []string{"my.env", ".environment", ".envexample", ".gitignore.bak", "MY_AGENTS.md", "AGENTS.md.bak", "docs/AGENTS.md/guide.txt"} {
		if got := builtin.Match(name); len(got) != 0 {
			t.Errorf("unrelated path %s: %v", name, got)
		}
	}
}

// +case:id=lockfile_tags,desc=`Known dependency lockfiles carry replaceable path tags`,expect=`Matcher and graph agree without treating locks as manifests`
func TestLockfileDocumentTags(t *testing.T) {
	matcher, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := cg.NewTagMatcher([]cg.TagRule{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"go.sum", "go.work.sum", "uv.lock", "poetry.lock", "Pipfile.lock", "pdm.lock", "pylock.toml", "pylock.production.toml", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "Cargo.lock", "Gemfile.lock", "composer.lock"}
	var docs []cg.Document
	for _, prefix := range []string{"", "nested/"} {
		for _, name := range names {
			doc := cg.Document{Path: prefix + name}
			tags := matcher.DocumentTags(doc)
			if !slices.Contains(tags, cg.LockfileTag) || slices.Contains(tags, cg.ManifestTag) {
				t.Errorf("%s: %v", doc.Path, tags)
			}
			if slices.Contains(disabled.DocumentTags(doc), cg.LockfileTag) {
				t.Errorf("disabled rule matched %s", doc.Path)
			}
			docs = append(docs, doc)
		}
	}
	for _, name := range []string{"app.lock", "uv.lock.bak", "my-go.sum", "pylock..toml", "uv.lock/source.py", "package.json"} {
		if slices.Contains(matcher.Match(name), cg.LockfileTag) {
			t.Errorf("unrelated path %s", name)
		}
	}
	graph, _, err := cg.Build(context.Background(), "locks", docs, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		node, ok := graph.Node(cg.DocumentID(doc.Path))
		if !ok || !reflect.DeepEqual(node.Tags, matcher.DocumentTags(doc)) {
			t.Errorf("graph tags for %s: %v", doc.Path, node.Tags)
		}
	}
	if got := matcher.Match("vendor/lib/Cargo.lock"); !slices.Contains(got, cg.LockfileTag) || !slices.Contains(got, cg.DependencyTag) {
		t.Fatal(got)
	}
}
