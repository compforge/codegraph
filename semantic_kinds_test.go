package codegraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/pkg"
)

// +case:id=semantic_use_kinds,expect=`Calls, callback reads, type uses and compound writes retain distinct dependencies without generic reference edges`
func TestSemanticUseKinds(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package app\nvar count int\ntype Model struct{}\nfunc handler(){}\nfunc register(f func()){}\nfunc entry(m Model){handler(); register(handler); count += 1; count = 2; _ = count}"},
		{"app.ts", "let count = 0; class Model {} function handler(){} function register(f: any){} function entry(m: Model){handler(); register(handler); count += 1; count = 2; return count}"},
		{"app.js", "let count = 0; function handler(){} function register(f){} function entry(){handler(); register(handler); count += 1; count = 2; return count}"},
		{"app.py", "count = 0\nclass Model: pass\ndef handler(): pass\ndef register(f): pass\ndef entry(m: Model):\n    count = 0\n    handler()\n    register(handler)\n    count += 1\n    count = 2\n    return count\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := Build(context.Background(), "semantic", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			entry := g.Find(tc.path, Function, "entry")[0]
			counts := map[string]int{}
			for _, e := range g.RelationsFrom(entry.ID) {
				n, _ := g.Node(e.Target)
				counts[string(e.Kind)+"/"+n.Name]++
			}
			wantCounts := map[string]int{"calls/handler": 1, "reads/handler": 1}
			if tc.path != "app.py" {
				wantCounts["reads/count"] = 2
				wantCounts["writes/count"] = 2
			}
			for key, want := range wantCounts {
				if counts[key] != want {
					t.Errorf("%s=%d want=%d; all=%v", key, counts[key], want, counts)
				}
			}
			if tc.path != "app.js" && counts["uses_type/Model"] != 1 {
				t.Errorf("type use: %v", counts)
			}
			for _, n := range g.Nodes() {
				if !n.Kind.IsPublic() {
					t.Fatalf("internal node escaped: %+v", n)
				}
			}
			for _, e := range g.Relations() {
				if !e.Kind.IsPublic() {
					t.Fatalf("internal relation escaped: %+v", e)
				}
			}
			// Assignment roles are determined at the operated-on token.
			at := strings.Index(tc.source, "count +=")
			kinds := map[RelationKind]bool{}
			for _, e := range g.RelationsFrom(entry.ID) {
				if e.Location.StartByte == at {
					kinds[e.Kind] = true
				}
			}
			if tc.path != "app.py" && (!kinds[Reads] || !kinds[Writes]) {
				t.Errorf("compound assignment: %v", kinds)
			}
		})
	}
}

func TestCapturedValueDependency(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"a.ts", "function outer(){ const value=1; function inner(){ return value; } return inner; }"},
		{"a.py", "def outer():\n    value = 1\n    def inner():\n        return value\n    return inner\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, _, err := Build(context.Background(), "capture", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{Admission: AnalysisAdmission()})
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, e := range g.Relations() {
				if e.Kind == Captures {
					source, _ := g.Node(e.Source)
					target, _ := g.Node(e.Target)
					found = source.Name == "inner" && target.Name == "value"
				}
			}
			if !found {
				t.Fatalf("capture bridge missing: %+v", g.Relations())
			}
		})
	}
}

// +case:id=internal_kind_publication,expect=`Open enums allow internal extensions but the publication boundary rejects them`
func TestInternalKindsCannotPublish(t *testing.T) {
	a := &graphAssembler{nodes: map[string]Node{}, relations: map[string]Relation{}}
	if err := a.addNode(Node{Kind: pkg.NodeKind("binding_candidate")}); err == nil {
		t.Fatal("internal node accepted")
	}
	if err := a.addRelation(Relation{Kind: analysis.NameUse}); err == nil {
		t.Fatal("internal relation accepted")
	}
}

func TestRawUseBudgetPreservesShadowing(t *testing.T) {
	doc := Document{Path: "a.go", Content: []byte("package app\nfunc target(){}\nfunc entry(target int){" + strings.Repeat("_ = target;", 100) + "}")}
	g, r, err := Build(context.Background(), "raw", []Document{doc}, Options{Admission: AnalysisAdmission(), Analysis: AnalysisLimits{Uses: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Analysis.RawUses != 100 || r.Analysis.Uses != 0 {
		t.Fatal(r.Analysis)
	}
	for _, e := range g.Relations() {
		if e.Kind.IsDependency() {
			t.Fatalf("shadowed outer target leaked: %+v", e)
		}
	}
	_, _, err = Build(context.Background(), "raw", []Document{doc}, Options{Analysis: AnalysisLimits{RawUses: 50}})
	var budget *BuildBudgetError
	if !errors.As(err, &budget) || budget.Resource != "RawUses" {
		t.Fatalf("raw work must remain bounded: %v", err)
	}
}

func TestPythonModuleWrites(t *testing.T) {
	source := "count = 0\ncount += 1\ncount = 2\nprint(count)\n"
	g, _, err := Build(context.Background(), "writes", []Document{{Path: "a.py", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := g.Find("a.py", Variable, "count")[0]
	if len(g.RelationsTo(count.ID, Reads)) != 2 || len(g.RelationsTo(count.ID, Writes)) != 2 {
		t.Fatal(g.RelationsTo(count.ID))
	}
}

// +case:id=composite_key_shadow,expect=`A same-spelled local value does not shadow the field key in a composite literal`
func TestCompositeKeyIsNotShadowOnly(t *testing.T) {
	source := "package app\ntype Box struct{ value int }\nfunc entry(){ value:=1; _ = Box{value:value} }"
	g, _, err := Build(context.Background(), "key", []Document{{Path: "a.go", Content: []byte(source)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	field := g.Find("a.go", Field, "Box.value")[0]
	edges := g.RelationsTo(field.ID, Writes)
	if len(edges) != 1 || edges[0].Location.StartByte != strings.Index(source, "value:value") {
		t.Fatal(edges)
	}
}
