package codegraph

import (
	"context"
	"strings"
	"testing"
)

func TestGoMemberReferenceReceivers(t *testing.T) {
	for _, tc := range []struct{ name, setup, expr, target string }{
		{"parameter", "", "box.Value", "Box.Value"},
		{"method-value", "", "box.Work", "Box.Work"},
		{"method-expression", "", "Box.Work", "Box.Work"},
		{"dereference", "", "(*box).Value", "Box.Value"},
		{"literal", "", "(Box{}).Value", "Box.Value"},
		{"new", "", "new(Box).Value", "Box.Value"},
		{"initialized", "local := &Box{};", "local.Value", "Box.Value"},
		{"alias-value", "local := box;", "local.Work", "Box.Work"},
		{"pointer-alias", "type Alias = *Box; var local Alias;", "local.Work", "Box.Work"},
		{"defined-pointer", "type Pointer *Box; var local Pointer;", "local.Value", "Box.Value"},
		{"alias-type", "type Alias = Box; var local Alias;", "local.Work", "Box.Work"},
		{"defined-type-field", "type Named Box; var local Named;", "local.Value", "Box.Value"},
		{"defined-type-method", "type Named Box; var local Named;", "local.Work", ""},
		{"shadow", "type Box struct{ Value int }; var local Box;", "local.Value", "Entry.Box.Value"},
		{"outer-type-identity", "local := box; type Box struct{ Wrong int };", "local.Value", "Box.Value"},
		{"generic", "local := Generic[int]{};", "local.Value", "Generic.Value"},
		{"interface", "var local Interface;", "local.Work", "Interface.Work"},
		{"defined-interface", "type Named Interface; var local Named;", "local.Work", "Interface.Work"},
		{"embedded-interface", "var local Combined;", "local.Work", "Interface.Work"},
		{"declared-return", "", "Make().Value", "Box.Value"},
		{"member-chain", "", "box.Child.Value", "Box.Value"},
		{"unknown-parameter", "", "unknown.Value", ""},
		{"builtin-spelling", "", "box.close", "Box.close"},
		{"deduplicate", "local := Box{}; local = Box{};", "local.Value", "Box.Value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `package app
 const Value=1
 func Work(){}
 type Box struct{ Value int; Child *Box; close func() }
 func (b Box) Work(){}
 type Generic[T any] struct { Value T }
 type Interface interface{ Work() }
 type Combined interface{ Interface }
 func Make() Box{return Box{}}
 func Entry(box *Box, unknown any){ ` + tc.setup + ` _ = ` + tc.expr + ` }
 `
			g, _, err := Build(context.Background(), "members", []Document{{Path: "app.go", Content: []byte(source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			selectorStart := strings.LastIndex(source, tc.expr) + strings.LastIndex(tc.expr, ".") + 1
			rows := query(t, g, `MATCH ()-[r:references]->(target) RETURN r,target`, nil)
			count := 0
			for _, row := range rows {
				edge, target := row["r"].(Relation), row["target"].(Node)
				if edge.Location.StartByte != selectorStart {
					continue
				}
				count++
				if target.QualifiedName != tc.target || edge.Confidence != Candidate || edge.Basis != "receiver_type" {
					t.Fatalf("unexpected member binding: %+v", row)
				}
			}
			if tc.target != "" {
				if count != 1 {
					t.Fatalf("member edges=%d, report=%+v", count, g.Report())
				}
			} else {
				if count != 0 {
					t.Fatal(rows)
				}
				localized := false
				for _, gap := range g.Report().Diagnostics {
					if gap.Code == "unresolved_reference" && gap.Location.StartByte == selectorStart {
						localized = true
					}
				}
				if !localized {
					t.Fatalf("missing member must retain a local diagnostic: %+v", g.Report())
				}
			}
		})
	}
}

func TestGoMemberReferencesImportedReload(t *testing.T) {
	ctx := context.Background()
	source := `package app
 import alias "example.org/types"
 func Entry(box *alias.Box){ _ = box.Value; _ = box.Work; _ = alias.Box.Work }
 `
	g, _, err := Build(ctx, "member-reload", []Document{{Path: "app.go", Content: []byte(source)}}, Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		count := 0
		for _, row := range query(t, g, `MATCH ()-[r:references]->(target) RETURN r,target`, nil) {
			e, n := row["r"].(Relation), row["target"].(Node)
			if e.Location.Path == "app.go" && (n.Kind == Field || n.Kind == Method) {
				count++
				if e.Confidence != Candidate || !strings.HasPrefix(n.Location.Path, "types/") {
					t.Fatal(row)
				}
			}
		}
		if count != want {
			t.Fatalf("member edges=%d want=%d", count, want)
		}
	}
	check(0)
	err = g.AddDocuments(ctx, Document{Path: "types/types.go", Content: []byte("package model; type Box struct { Value int }")}, Document{Path: "types/methods.go", Content: []byte("package model; func (b *Box) Work(){}")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	check(3)
}

func TestGoMemberReferencesScopeAndAmbiguity(t *testing.T) {
	docs := []Document{
		{Path: "use.go", Content: []byte("package app; func Entry(box *Box){ _ = box.Value }")},
		{Path: "a.go", Content: []byte("package app; type Box struct{ Value int }")},
		{Path: "b.go", Content: []byte("package app; type Box struct{ Value string }")},
		{Path: "other/box.go", Content: []byte("package other; type Box struct{ Value int }")},
		{Path: "extra_test.go", Content: []byte("package app; type Box struct{ Value int }")},
	}
	g, _, err := Build(context.Background(), "ambiguous-members", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:references]->(target:Field) RETURN r,target`, nil)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		e, n := row["r"].(Relation), row["target"].(Node)
		if e.Confidence != Candidate || (n.Location.Path != "a.go" && n.Location.Path != "b.go") {
			t.Fatal(row)
		}
	}
	limited, err := New("member-budget", Options{MaxRelations: len(g.Relations()) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(context.Background(), docs...); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(context.Background()); err == nil || len(limited.Nodes()) != 0 {
		t.Fatalf("member relations must respect atomic budget: %v", err)
	}
}
