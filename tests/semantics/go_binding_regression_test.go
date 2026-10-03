package semantics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

func TestGoChainedCallIdentity(t *testing.T) {
	source := `package app
 type Box struct{}
 func Make() Box { return Box{} }
 func (b Box) Next() Box { return b }
 func Entry() {
  // 多字节前缀不能改变 byte span 的含义。
  Make().Next().Next()
  maker := Make
  maker().Next()
  (Box{}).Next()
 }
 `
	g, _, err := buildTestBuilder(context.Background(), "chained", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "app.go", Content: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Make()": "Make", "Make().Next()": "Next", "Make().Next().Next()": "Next", "maker()": "maker", "maker().Next()": "Next", "(Box{}).Next()": "Next"}
	for _, call := range facts.Calls {
		text := source[call.Location.StartByte:call.Location.EndByte]
		if call.Name != want[text] {
			t.Fatalf("%q: name=%q want=%q", text, call.Name, want[text])
		}
		delete(want, text)
	}
	if len(want) != 0 {
		t.Fatalf("missing calls: %v", want)
	}
	rows := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[r:calls]->(target) RETURN r,target`, nil)
	found := map[string]bool{}
	for _, row := range rows {
		edge, target := row["r"].(codegraph.Relation), row["target"].(codegraph.Node)
		text := source[edge.Location.StartByte:edge.Location.EndByte]
		if strings.HasSuffix(text, ".Next()") && target.Name != "Next" {
			t.Fatalf("outer call bound to inner: %s %+v", text, row)
		}
		if text == "Make()" && target.Name == "Make" && edge.Confidence == codegraph.Exact {
			found[text] = true
		}
		if text == "maker()" && target.Name == "Make" && edge.Confidence == codegraph.Scoped {
			found[text] = true
		}
		if text == "(Box{}).Next()" && target.Name == "Next" {
			found[text] = true
		}
	}
	if len(found) != 3 {
		t.Fatalf("lost known calls: %v", rows)
	}
	for _, outer := range []string{"Make().Next()", "Make().Next().Next()", "maker().Next()"} {
		start := strings.Index(source, outer)
		covered := false
		for _, d := range g.Report().Diagnostics {
			if d.Relation == codegraph.Calls && d.Location.StartByte == start && d.Location.EndByte == start+len(outer) {
				covered = true
			}
		}
		for _, row := range rows {
			e := row["r"].(codegraph.Relation)
			if e.Location.StartByte == start && e.Location.EndByte == start+len(outer) {
				covered = true
			}
		}
		if !covered {
			t.Fatalf("silent chained-call gap: %s", outer)
		}
	}
}

func TestGoCompositeKeyBinding(t *testing.T) {
	for _, tc := range []struct {
		name, types, expression, extra string
		kind                           codegraph.NodeKind
		target                         string
	}{
		{"struct", "type S struct { Key int }", "S{Key: Key}", "", codegraph.Field, "Key"},
		{"alias", "type S struct { Key int }; type A = S", "A{Key: Key}", "", codegraph.Field, "Key"},
		{"defined", "type S struct { Key int }; type A S", "A{Key: Key}", "", codegraph.Field, "Key"},
		{"generic", "type S[T any] struct { Key T }", "S[int]{Key: Key}", "", codegraph.Field, "Key"},
		{"map", "", "map[int]int{Key: Key}", "", codegraph.Constant, "Key"},
		{"named-map", "type M map[int]int; type A = M", "A{Key: Key}", "", codegraph.Constant, "Key"},
		{"array", "", "[4]int{Key: Key}", "", codegraph.Constant, "Key"},
		{"slice", "type A []int", "A{Key: Key}", "", codegraph.Constant, "Key"},
		{"cross-file-map", "", "M{Key: Key}", "package app; type M map[int]int", codegraph.Constant, "Key"},
		{"cross-file-struct", "", "S{Key: Key}", "package app; type S struct { Key int }", codegraph.Field, "Key"},
		{"cross-file-alias", "type A = S", "A{Key: Key}", "package app; type S struct { Key int }", codegraph.Field, "Key"},
		{"unknown", "", "Missing{Key: Key}", "", "", ""},
		{"cycle", "type S A; type A S", "S{Key: Key}", "", "", ""},
		{"anonymous", "", "struct{ Key int }{Key: Key}", "", "", ""},
		{"elided", "type S struct { Key int }", "[]S{{Key: Key}}", "", "", ""},
		{"local-shadow", "type S struct { Other int }", "func() { type S struct { Key int }; _ = S{Key: Key} }", "", codegraph.Field, "Key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package app\nconst Key = 1\n" + tc.types + "\nfunc Entry(){ _ = " + tc.expression + " }\n"
			docs := []codegraph.Document{{Path: "app.go", Content: []byte(source)}}
			if tc.extra != "" {
				docs = append(docs, codegraph.Document{Path: "types.go", Content: []byte(tc.extra)})
			}
			g, _, err := codegraph.Build(context.Background(), "keys", docs, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			keyStart := strings.Index(source, "Key: Key")
			valueStart := keyStart + 5
			keyEdges, valueEdges := 0, 0
			rows := query(t, g, `MATCH ()-[r:references]->(target) RETURN r,target`, nil)
			for _, row := range rows {
				e, n := row["r"].(codegraph.Relation), row["target"].(codegraph.Node)
				if e.Location.Path != "app.go" {
					continue
				}
				switch e.Location.StartByte {
				case keyStart:
					keyEdges++
					if n.Kind != tc.kind || n.Name != tc.target {
						t.Fatalf("wrong key binding: %+v", row)
					}
					if n.Kind == codegraph.Field && (e.Confidence != codegraph.Scoped || e.Evidence[0].Basis != "composite_field") {
						t.Fatal(e)
					}
				case valueStart:
					valueEdges++
					if n.Kind != codegraph.Constant || n.Name != "Key" || e.Confidence != codegraph.Exact {
						t.Fatalf("value binding changed: %+v", row)
					}
				}
			}
			if valueEdges != 1 {
				t.Fatalf("value edges=%d", valueEdges)
			}
			if tc.kind != "" {
				if keyEdges != 1 {
					t.Fatalf("key edges=%d, report=%+v", keyEdges, g.Report())
				}
			} else {
				if keyEdges != 0 {
					t.Fatalf("unknown key guessed a target: %v", rows)
				}
				gap := false
				for _, d := range g.Report().Diagnostics {
					if d.Code == "unresolved_reference" && d.Location.StartByte == keyStart {
						gap = true
					}
				}
				if !gap {
					t.Fatalf("unknown key lost without diagnostic: %+v", g.Report())
				}
			}
		})
	}
}

func TestGoCompositeKeyImportedTypeAndReload(t *testing.T) {
	source := `package app
 import alias "example.org/types"
 const Key = 1
 func Entry(){ _ = alias.S{Key: Key} }
 `
	g, _, err := buildTestBuilder(context.Background(), "imported-keys", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	keyStart := strings.Index(source, "Key: Key")
	check := func(want int) {
		t.Helper()
		count := 0
		for _, row := range query(t, g.Result(), `MATCH ()-[r:references]->(target) RETURN r,target`, nil) {
			e, n := row["r"].(codegraph.Relation), row["target"].(codegraph.Node)
			if e.Location.Path == "app.go" && e.Location.StartByte == keyStart {
				count++
				if n.Kind != codegraph.Field || n.Location.Path != "types/types.go" {
					t.Fatal(row)
				}
			}
		}
		if count != want {
			t.Fatalf("field edges=%d want=%d", count, want)
		}
	}
	check(0)
	err = g.AddDocuments(context.Background(), codegraph.Document{Path: "types/types.go", Content: []byte("package different\ntype S struct { Key int }\n")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	check(1)
}

func TestGoCompositeKeyRelationBudget(t *testing.T) {
	ctx := context.Background()
	doc := codegraph.Document{Path: "app.go", Content: []byte("package app; type S struct { Key int }; func Entry(){ _ = S{Key: 1} }")}
	complete, _, err := codegraph.Build(ctx, "complete-keys", []codegraph.Document{doc}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	limited, err := codegraph.NewBuilder("limited-keys", codegraph.Options{MaxRelations: len(complete.Relations()) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(ctx); err == nil || len(limited.Result().Nodes()) != 0 {
		t.Fatalf("field-key relation budget must reject the batch atomically: %v", err)
	}
}

func TestGoCompositePredeclaredKeyContexts(t *testing.T) {
	source := []byte(`package app
 type M map[any]int
 type Alias = M
 type S struct { nil int; len int }
 func Entry(){
  _ = map[any]int{nil: 1, true: 2}
  _ = Alias{nil: 1, false: 2}
  _ = S{nil: 1, len: 2}
 }
 `)
	g, err := codegraph.NewBuilder("predeclared-keys", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "app.go", Content: source})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, ref := range facts.References {
		counts[ref.Name]++
	}
	if counts["nil"] != 1 || counts["len"] != 1 || counts["true"] != 0 || counts["false"] != 0 {
		t.Fatal(counts)
	}
}
