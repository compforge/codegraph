package codegraph_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
)

// +case:id=test_document_tags,desc=`Test naming uses shared language suffixes`,expect=`Matcher and graph agree; fixture/support classification and rule replacement remain independent`
func TestTestDocumentTags(t *testing.T) {
	matcher, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := cg.NewTagMatcher([]cg.TagRule{})
	custom, _ := cg.NewTagMatcher([]cg.TagRule{{Name: cg.TestTag, Pattern: `^checks/custom\.ts$`}})
	positives := []string{"unit_test.go", "lib/unit_test.go", "test_unit.py", "tests/unit_test.py", "a.test.js", "a.spec.jsx", "a.test.mjs", "a.spec.cjs", "a.test.ts", "a.spec.tsx", "a.test.mts", "a.spec.cts", "a.test.es6", "a.spec.sjs", "a.test.njs", "src/__tests__/case.cts", "src/__tests__/nested/case.mts", "fixtures/a.test.mts", "vendor/a.test.cts"}
	negatives := []string{"unit.go", "conftest.py", "tests/helper.py", "test_unit.pyi", "a.test.json", "a.spec.md", "a.test.unknown", "src/__tests__/readme.md", "src/__tests__", "src/__tests__/data.json", "my__tests__/helper.ts", "a.test.ts.map", "a_test.go.bak"}
	var docs []cg.Document
	for _, group := range []struct {
		paths []string
		test  bool
	}{{positives, true}, {negatives, false}} {
		for _, name := range group.paths {
			doc := cg.Document{Path: name}
			tags := matcher.DocumentTags(doc)
			if slices.Contains(tags, cg.TestTag) != group.test {
				t.Errorf("%s: %v", name, tags)
			}
			if slices.Contains(disabled.DocumentTags(doc), cg.TestTag) || slices.Contains(custom.DocumentTags(doc), cg.TestTag) {
				t.Errorf("default test rules not replaced for %s", name)
			}
			docs = append(docs, doc)
		}
	}
	if !slices.Contains(custom.Match("checks/custom.ts"), cg.TestTag) {
		t.Fatal("custom test rule lost")
	}
	g, _, err := cg.Build(context.Background(), "tags", docs, cg.Options{Admission: &cg.AdmissionPolicy{ParseTags: []cg.Tag{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		n, ok := g.Document(doc.Path)
		if !ok || !reflect.DeepEqual(n.Tags, matcher.DocumentTags(doc)) {
			t.Fatalf("graph tags for %s: %+v", doc.Path, n)
		}
	}
	tags := matcher.DocumentTags(cg.Document{Path: "fixtures/a.test.mts"})
	for _, tag := range []cg.Tag{cg.SourceTag, cg.TestTag, cg.TestFixtureTag} {
		if !slices.Contains(tags, tag) {
			t.Fatalf("composed tags: %v", tags)
		}
	}
}

func TestTestTagAdmissionIsExplicit(t *testing.T) {
	docs := []cg.Document{{Path: "app.ts", Content: []byte("export function work() {}")}, {Path: "app.test.mts", Content: []byte("export function check() {}")}}
	g, _, err := cg.Build(context.Background(), "all", docs, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Document("app.test.mts"); !ok {
		t.Fatal("test tag excluded source by default")
	}
	g, _, err = cg.Build(context.Background(), "exclude", docs, cg.Options{Admission: &cg.AdmissionPolicy{ExcludedTags: []cg.Tag{cg.TestTag}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Document("app.test.mts"); ok {
		t.Fatal("test exclusion ignored")
	}
	if _, ok := g.Document("app.ts"); !ok {
		t.Fatal("production source excluded")
	}
}

func TestTagLanguageFilterIsRetained(t *testing.T) {
	rules := []cg.TagRule{{Name: cg.TestTag, Pattern: `^checks/`, Languages: []string{"typescript"}}}
	matcher, err := cg.NewTagMatcher(rules)
	if err != nil {
		t.Fatal(err)
	}
	rules[0].Languages[0] = "javascript"
	for _, name := range []string{"checks/custom.ts", "checks/custom.mts", "checks/custom.cts"} {
		if !slices.Contains(matcher.Match(name), cg.TestTag) {
			t.Fatalf("lost captured language filter for %s", name)
		}
	}
	for _, name := range []string{"checks", "checks/custom.js", "checks/data.json", "checks/unknown.xyz"} {
		if slices.Contains(matcher.Match(name), cg.TestTag) {
			t.Fatalf("language filter ignored for %s", name)
		}
	}
}
