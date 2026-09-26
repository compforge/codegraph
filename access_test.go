package codegraph

import (
	"context"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestConsumerAccessors(t *testing.T) {
	source := fstest.MapFS{"main.go": {Data: []byte(`package p
type Box struct{}
func (b Box) Run() { Work(); Work() }
func Work() {}
`)}}
	g, report, err := Build(context.Background(), "rev", documents(source, "main.go"), Options{})
	if err != nil || len(report.Diagnostics) != 0 {
		t.Fatal(report, err)
	}
	methods := g.Find("main.go", Method, "Box.Run")
	if len(methods) != 1 || methods[0].Location.EndLine != 3 || methods[0].Location.EndColumn == 0 {
		t.Fatal(methods)
	}
	if got := g.Find("main.go", Function, "Work"); len(got) != 1 || got[0].Location.Line != 4 {
		t.Fatal(got)
	}
	node, ok := g.Node(methods[0].ID)
	if !ok || !reflect.DeepEqual(node, methods[0]) {
		t.Fatal(node, methods[0], ok)
	}
	edges := g.RelationsFrom(methods[0].ID, Calls)
	if len(edges) != 2 {
		t.Fatal(edges)
	}
	target, ok := g.Node(edges[0].Target)
	if !ok || target.QualifiedName != "Work" {
		t.Fatal(target, ok)
	}
	if len(g.RelationsTo(target.ID, Calls)) != 2 {
		t.Fatal(g.RelationsTo(target.ID, Calls))
	}
	if len(g.Find("main.go", Interface, "Box.Run")) != 0 {
		t.Fatal("kind filter ignored")
	}
}
