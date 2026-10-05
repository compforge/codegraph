package analysis

import "testing"

func TestGoModuleContextSharedByOrganizationAndImports(t *testing.T) {
	original := ResolutionContext{GoModules: map[string]string{"nested": "example.org/child"}}
	normalized := original.WithGoModule("example.org/root")
	if original.GoModules["."] != "" {
		t.Fatal("mutated supplied context")
	}
	for _, tc := range []struct{ dir, imported, root, module string }{
		{"lib", "example.org/root/lib", ".", "example.org/root"},
		{"nested/lib", "example.org/child/lib", "nested", "example.org/child"},
	} {
		root, module, ok := normalized.GoModule(tc.dir)
		if !ok || root != tc.root || module != tc.module {
			t.Fatal(root, module, ok)
		}
		dir, ok := normalized.GoImportDir(tc.imported)
		if !ok || dir != tc.dir {
			t.Fatal(dir, ok)
		}
		if got := normalized.GoPackage(tc.dir, "p"); got != tc.imported {
			t.Fatal(got)
		}
	}
	if _, ok := normalized.GoImportDir("example.org/root/nested/lib"); ok {
		t.Fatal("parent import crossed child module boundary")
	}
	override := ResolutionContext{GoModules: map[string]string{".": "example.org/explicit", "a": "example.org/short"}}.WithGoModule("example.org/ignored")
	if _, ok := override.GoImportDir("example.org/ignored/lib"); ok {
		t.Fatal("explicit root ignored")
	}
	for i := 0; i < 50; i++ {
		root, module, ok := override.GoModule("a/lib")
		if !ok || root != "a" || module != "example.org/short" {
			t.Fatal(root, module, ok)
		}
	}
}
