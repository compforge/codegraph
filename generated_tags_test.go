package codegraph_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
)

// +case:id=generated_document_tags,desc=`Generated output conventions work without parser support`,expect=`Matcher and graph agree; handwritten suffixes remain admitted and rules remain replaceable`
func TestGeneratedDocumentTags(t *testing.T) {
	matcher, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := cg.NewTagMatcher([]cg.TagRule{})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := cg.NewTagMatcher([]cg.TagRule{{Name: cg.GeneratedTag, Pattern: `^custom/output\.rs$`}})
	if err != nil {
		t.Fatal(err)
	}
	positives := []string{
		"api.generated.ts", "api.generated.rs", "api.gen.go", "api.pb.go", "api_grpc.pb.go", "api.pb.gw.go",
		"api_pb2.py", "api_pb2.pyi", "api_pb2_grpc.py", "api_pb2_grpc.pyi",
		"api.pb.cc", "api.pb.h", "api.grpc.pb.cc", "api.grpc.pb.h", "api.pb-c.c", "api.pb-c.h",
		"runtime.js.map", "runtime.jsx.map", "runtime.mjs.map", "runtime.cjs.map", "style.css.map",
		"api.d.ts.map", "api.d.mts.map", "api.d.cts.map", "kitex_gen/api/service.go",
	}
	negatives := []string{
		"runtime.js", "runtime.mjs", "runtime.cjs", "style.css", "api.ts", "api.d.ts", "api.d.mts", "api.d.cts",
		"api.py", "api.pyi", "api.proto", "api.go", "api.c", "api.cc", "api.h", "api.rs", "Api.java",
		"world.map", "api.json.map", "api_pb2.py.bak", "api.pb.gw.go.bak", "runtime.js.map.bak",
		"runtime.js.map/readme.txt", "mykitex_gen/service.go",
	}
	var docs []cg.Document
	for _, group := range []struct {
		paths     []string
		generated bool
	}{{positives, true}, {negatives, false}} {
		for _, name := range group.paths {
			for _, prefix := range []string{"", "nested/"} {
				doc := cg.Document{Path: prefix + name}
				tags := matcher.DocumentTags(doc)
				if slices.Contains(tags, cg.GeneratedTag) != group.generated {
					t.Errorf("%s: tags = %v, generated = %v", doc.Path, tags, group.generated)
				}
				if slices.Contains(disabled.DocumentTags(doc), cg.GeneratedTag) || slices.Contains(custom.DocumentTags(doc), cg.GeneratedTag) {
					t.Errorf("default generated rules not replaced for %s", doc.Path)
				}
				docs = append(docs, doc)
			}
		}
	}
	if !slices.Contains(custom.Match("custom/output.rs"), cg.GeneratedTag) {
		t.Fatal("custom generated rule lost")
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
	for _, tc := range []struct {
		path string
		want []cg.Tag
	}{
		{"devloop/dist/hooks/runtime.js.map", []cg.Tag{cg.BuildOutputTag, cg.GeneratedTag}},
		{"api.pb.gw.go", []cg.Tag{cg.GeneratedTag, cg.SourceTag}},
		{"api_pb2.py", []cg.Tag{cg.GeneratedTag, cg.SourceTag}},
	} {
		if got := matcher.DocumentTags(cg.Document{Path: tc.path}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestGeneratedSuffixAdmission(t *testing.T) {
	docs := []cg.Document{
		{Path: "runtime.js", Content: []byte("export function work() {}")},
		{Path: "runtime.js.map", Content: []byte(`{"version":3,"sources":["runtime.ts"],"mappings":""}`)},
		{Path: "api.pb.gw.go", Content: []byte("package api\n")},
		{Path: "api_pb2.py", Content: []byte("value = 1\n")},
	}
	for _, exclude := range []bool{false, true} {
		opts := cg.Options{}
		if exclude {
			opts.Admission = cg.AnalysisAdmission()
		}
		g, _, err := cg.Build(context.Background(), "generated", docs, opts)
		if err != nil {
			t.Fatal(err)
		}
		for i, doc := range docs {
			_, ok := g.Document(doc.Path)
			if want := !exclude || i == 0; ok != want {
				t.Errorf("%s: admitted = %v, want %v (exclusion = %v)", doc.Path, ok, want, exclude)
			}
		}
	}
}
