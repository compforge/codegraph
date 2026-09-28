package codegraph

import (
	"context"
	"strings"
	"testing"
)

func TestLexicalControlBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"catch", "function work(){} function entry(){try{}catch(work){work();}work();}", 1},
		{"for let", "function work(){} function entry(){for(const work of []){work();}work();}", 1},
		{"block let", "function work(){} function entry(){{let work:any;work();}work();}", 1},
		{"block var", "function work(){} function entry(){{var work:any;}work();}", 0},
		{"destructure", "function work(){} function entry({work}:any){work();}", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := Build(context.Background(), tc.name, []Document{{Path: "a.ts", Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			got := 0
			for _, r := range g.Relations() {
				if r.Kind == Calls && r.Confidence == Exact {
					got++
				}
			}
			if got != tc.want {
				t.Fatalf("calls=%d want=%d", got, tc.want)
			}
		})
	}
}
func TestReceiverHintsFollowVisibleBinding(t *testing.T) {
	source := "class A{run(){}} class B{run(){}} function entry(){let x=new A(); {let x=new B(); x.run();} x.run();}"
	g, _, err := Build(context.Background(), "receiver", []Document{{Path: "a.ts", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, last := strings.Index(source, "x.run()"), strings.LastIndex(source, "x.run()")
	seen := map[int]string{}
	for _, r := range g.Relations() {
		if r.Kind == Calls && (r.Location.StartByte == first || r.Location.StartByte == last) {
			n, _ := g.Node(r.Target)
			if seen[r.Location.StartByte] != "" {
				t.Fatal("shadowed receiver leaked", r)
			}
			seen[r.Location.StartByte] = n.QualifiedName
		}
	}
	if seen[first] != "B.run" || seen[last] != "A.run" {
		t.Fatal(seen)
	}
}

func TestDestructuringPropertyDoesNotBindLocalName(t *testing.T) {
	source := "function entry(input:any){const {target:local}=input; const target=1;return target}"
	g, _, err := Build(context.Background(), "pattern", []Document{{Path: "a.ts", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	property := strings.Index(source, "target:")
	for _, r := range g.Relations() {
		if r.Kind == References && r.Location.StartByte == property {
			t.Fatal("property bound as local variable", r)
		}
	}
	localized := false
	for _, d := range g.Report().Diagnostics {
		if d.Code == "unresolved_reference" && d.Location.StartByte == property {
			localized = true
		}
	}
	if !localized {
		t.Fatal("missing local property evidence gap")
	}
}
