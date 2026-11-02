package codegraph_test

import (
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
)

// +case:id=shared_material_conventions,desc=`Repository consumers share material naming conventions`,expect=`Known artifacts and tests are tagged without misclassifying authored patches or ambiguous directories`
func TestSharedMaterialConventions(t *testing.T) {
	matcher, err := cg.NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		tag  cg.Tag
	}{
		{"api.pb.gw.go", cg.GeneratedTag}, {"schema_capnp.py", cg.GeneratedTag},
		{"tests/test_service.py", cg.TestTag}, {"service_test.py", cg.TestTag},
		{"Bar_Test.go", cg.TestTag}, {"parser_test.rs", cg.TestTag}, {"user_spec.rb", cg.TestTag},
		{"FooTest.java", cg.TestTag}, {"FooTests.java", cg.TestTag},
		{"src/test/kotlin/Foo.kt", cg.TestTag}, {"src/test/java/Foo.java", cg.TestTag},
		{"Component.test.ets", cg.TestTag}, {"app.test.ts", cg.TestTag},
		{"backend/.venv/lib/a.py", cg.DependencyTag}, {"ios/Pods/Lib/a.swift", cg.DependencyTag},
		{"oh_modules/lib/a.ets", cg.DependencyTag}, {".yarn/cache/a.js", cg.DependencyTag},
		{".yarn/unplugged/a.js", cg.DependencyTag}, {".pnp.cjs", cg.DependencyTag}, {"foo.egg-info/PKG-INFO", cg.DependencyTag},
		{"web/.nuxt/app.js", cg.BuildOutputTag}, {"target/a.go", cg.BuildOutputTag}, {"obj/a.cs", cg.BuildOutputTag},
		{"coverage/lcov-report/index.html", cg.BuildOutputTag},
		{"infra/.terraform/modules/a.tf", cg.CacheTag}, {".turbo/state.json", cg.CacheTag},
		{".gradle/cache.bin", cg.CacheTag}, {".tox/a.py", cg.CacheTag}, {".svn/entries", cg.ToolingTag},
		{"bundle.MIN.js", cg.MinifiedTag},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if tags := matcher.DocumentTags(cg.Document{Path: tc.path}); !slices.Contains(tags, tc.tag) {
				t.Fatalf("tags=%v, want %s", tags, tc.tag)
			}
		})
	}
	for _, name := range []string{"rpm/source.go", "pkgs/source.go", "_packages/source.go", ".yarn/patches/fix.patch", ".yarn/custom/app.js", "src/generated/app.go", "src/vendor_client.go"} {
		tags := matcher.DocumentTags(cg.Document{Path: name})
		for _, tag := range []cg.Tag{cg.GeneratedTag, cg.DependencyTag, cg.BuildOutputTag} {
			if slices.Contains(tags, tag) {
				t.Errorf("authored path %s tagged %s", name, tag)
			}
		}
	}
}
