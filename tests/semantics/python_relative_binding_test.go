package semantics_test

import (
	"context"
	cg "github.com/compforge/codegraph"
	"testing"
)

func TestBareRelativePackageExports(t *testing.T) {
	for _, prefix := range []string{".", ".."} {
		t.Run(prefix, func(t *testing.T) {
			caller := "pkg/main.py"
			if prefix == ".." {
				caller = "pkg/sub/main.py"
			}
			docs := []cg.Document{
				{Path: caller, Content: []byte("from " + prefix + " import run as invoke\ndef entry():\n    invoke()\n")},
				{Path: "pkg/__init__.py", Content: []byte("from .impl import work as run\n")},
				{Path: "pkg/impl.py", Content: []byte("def work(): pass\n")},
				{Path: "other/impl.py", Content: []byte("def work(): pass\n")},
			}
			b, _, err := buildTestBuilder(context.Background(), "relative", docs, cg.Options{})
			if err != nil {
				t.Fatal(err)
			}
			g := b.Result()
			source := g.Find(caller, cg.Function, "entry")
			target := g.Find("pkg/impl.py", cg.Function, "work")
			if len(source) != 1 || len(target) != 1 {
				t.Fatal(source, target)
			}
			calls := g.RelationsFrom(source[0].ID, cg.Calls)
			if len(calls) != 1 || calls[0].Target != target[0].ID || calls[0].Confidence != cg.Exact {
				t.Fatal(calls)
			}
		})
	}
}
