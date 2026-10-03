package module

import (
	"context"
	"reflect"
	"testing"

	"github.com/compforge/codegraph/internal/analysis"
)

func TestReceiverEvidenceSurvivesEarlierNameMatch(t *testing.T) {
	f := analysis.Facts{Path: "a.ts", Declarations: []analysis.Declaration{
		{Name: "Box", Kind: "class", Parent: -1, Span: analysis.Span{End: 30}},
		{Name: "run", Kind: "method", Parent: 0, Span: analysis.Span{Start: 2, End: 8}},
	}}
	files := map[string]analysis.Facts{f.Path: f}
	build := func(hints []analysis.CallTarget) analysis.Edge {
		t.Helper()
		x := analysis.NewIndex(files)
		if err := x.AddSources(context.Background(), []string{f.Path}); err != nil {
			t.Fatal(err)
		}
		x.Roots[f.Path] = analysis.DocumentRef(f.Path)
		if err := x.AttachDeclarations(context.Background(), []string{f.Path}); err != nil {
			t.Fatal(err)
		}
		methods := &methodIndex{namespaces: &NamespaceIndex{Index: x}, shared: analysis.NewMethodIndex(x)}
		call := analysis.Call{Name: "run", Span: analysis.Span{Start: 10, End: 20}, Targets: hints}
		edges, err := resolveCallTargets(context.Background(), f, call, files, "", methods, 10)
		if err != nil || len(edges) != 2 {
			t.Fatal(edges, err)
		}
		for _, e := range edges {
			x.Add(e)
		}
		for _, e := range x.Edges {
			if e.Kind == "calls" {
				if e.Confidence != analysis.Scoped || len(e.Evidence) != 2 {
					t.Fatal(e)
				}
				return e
			}
		}
		t.Fatal("missing call")
		return analysis.Edge{}
	}
	name := analysis.CallTarget{Name: "run", Kind: "method", Basis: "method_name"}
	typed := analysis.CallTarget{Name: "run", Kind: "method", ReceiverType: "Box", Basis: "receiver_type"}
	a, b := build([]analysis.CallTarget{name, typed}), build([]analysis.CallTarget{typed, name})
	if !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
}
