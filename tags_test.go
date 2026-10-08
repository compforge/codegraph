package codegraph_test

import (
	"context"
	"fmt"
	"reflect"
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
					if got := m.Match(node.Path); !reflect.DeepEqual(got, node.Tags) {
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
