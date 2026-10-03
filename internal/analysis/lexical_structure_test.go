package analysis

import (
	"context"
	"testing"
)

func TestRejectInvalidLexicalParents(t *testing.T) {
	for _, decls := range [][]Declaration{
		{{Name: "self", Parent: 0, Span: Span{End: 10}}},
		{{Name: "missing", Parent: 2, Span: Span{End: 10}}},
		{{Name: "outer", Parent: 1, Span: Span{End: 20}}, {Name: "inner", Parent: 0, Span: Span{Start: 5, End: 10}}},
		{{Name: "sibling", Parent: 1, Span: Span{End: 10}}, {Name: "other", Parent: -1, Span: Span{Start: 20, End: 30}}},
	} {
		x := NewIndex(map[string]Facts{"a.ts": {Path: "a.ts", Declarations: decls}})
		if err := x.AttachDeclarations(context.Background(), []string{"a.ts"}); err == nil {
			t.Fatal("accepted invalid lexical parent", decls)
		}
	}
}
