package codegraph

import (
	"context"
	"strings"
	"testing"
)

func TestGoMemberTypePropagation(t *testing.T) {
	for _, tc := range []struct{ name, setup, expr, suffix, target string }{
		{"chain", "", "box.Child.Name", "", "Leaf.Name"},
		{"assigned-chain", "child:=box.Child;", "child.Name", "", "Leaf.Name"},
		{"slice-index", "", "box.Items[0].Name", "", "Leaf.Name"},
		{"slice-view", "", "box.Items[:][0].Name", "", "Leaf.Name"},
		{"map-index", "", "box.Table[Leaf{}].Name", "", "Leaf.Name"},
		{"make", "items:=make([]Leaf,2);", "items[0].Name", "", "Leaf.Name"},
		{"append", "items:=append(box.Items,Leaf{});", "items[0].Name", "", "Leaf.Name"},
		{"range-value", "for _,item:=range box.Items {", "item.Name", "}", "Leaf.Name"},
		{"range-map-key", "for item:=range box.Table {", "item.Name", "}", "Leaf.Name"},
		{"range-map-value", "for _,item:=range box.Table {", "item.Name", "}", "Leaf.Name"},
		{"range-channel", "for item:=range box.Stream {", "item.Name", "}", "Leaf.Name"},
		{"range-array-index", "for index:=range box.Items {", "index.Name", "}", ""},
		{"named-container", "var items Items; for _,item:=range items {", "item.Name", "}", "Leaf.Name"},
		{"nested-range", "for _,items:=range box.Matrix { for _,item:=range items {", "item.Name", "}}", "Leaf.Name"},
		{"assertion", "", "unknown.(Leaf).Name", "", "Leaf.Name"},
		{"comma-ok-assertion", "item,ok:=unknown.(Leaf);_=ok;", "item.Name", "", "Leaf.Name"},
		{"comma-ok-map", "item,ok:=box.Table[Leaf{}];_=ok;", "item.Name", "", "Leaf.Name"},
		{"cyclic-assignment", "node:=Node{};node=node.Next;", "node.Next.Next.Name", "", "Node.Name"},
		{"cyclic-type", "var items Cycle;", "items[0].Name", "", ""},
		{"declared-return", "", "Make().Name", "", "Leaf.Name"},
		{"receive-gap", "var stream Stream;", "(<-stream).Special", "", ""},
		{"generic-type", "", "Generic[int].Work", "", "Generic.Work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `package app
 const Name=1
 type Leaf struct{Name int}
 type Node struct{Name int;Next *Node}
 type Box struct{Child *Leaf;Items []Leaf;Table map[Leaf]Leaf;Stream chan Leaf;Matrix [][]Leaf}
 type Items []Leaf
 type Cycle Other
 type Other Cycle
 type Stream chan Leaf
 func (Stream) Special(){}
 type Generic[T any] struct{}
 func (Generic[T]) Work(){}
 func Make() Leaf{return Leaf{}}
 func Entry(box *Box,unknown any){ ` + tc.setup + ` _ = ` + tc.expr + `; ` + tc.suffix + ` }
 `
			g, _, err := Build(context.Background(), "propagation", []Document{{Path: "app.go", Content: []byte(source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			start := strings.LastIndex(source, tc.expr) + strings.LastIndex(tc.expr, ".") + 1
			count := 0
			for _, row := range query(t, g, `MATCH ()-[r:references]->(target) RETURN r,target`, nil) {
				e, n := row["r"].(Relation), row["target"].(Node)
				if e.Location.StartByte != start {
					continue
				}
				count++
				if n.QualifiedName != tc.target || e.Confidence != Scoped {
					t.Fatalf("wrong propagation: %+v", row)
				}
			}
			if tc.target != "" {
				if count != 1 {
					t.Fatalf("edges=%d report=%+v", count, g.Report())
				}
			} else {
				if count != 0 {
					t.Fatal("unknown type guessed a target")
				}
				gap := false
				for _, d := range g.Report().Diagnostics {
					if d.Code == "unresolved_reference" && d.Location.StartByte == start {
						gap = true
					}
				}
				if !gap {
					t.Fatal("unresolved propagation lost its local diagnostic")
				}
			}
		})
	}
}

func TestGoMemberPropagationDeclarationContext(t *testing.T) {
	ctx := context.Background()
	source := `package app
 import lib "example.org/middle"
 import typed "example.org/decoy"
 var decoy typed.Leaf
 func Entry(box *lib.Box){ _ = box.Items[0].Name; _ = box.Child.Name }
 `
	docs := []Document{
		{Path: "app.go", Content: []byte(source)},
		{Path: "middle/type.go", Content: []byte("package middle; import typed \"example.org/end\"; type Box struct{ Items []typed.Leaf; Child *typed.Leaf }")},
		{Path: "decoy/type.go", Content: []byte("package decoy; type Leaf struct{Name string}")},
	}
	g, _, err := Build(ctx, "context", docs, Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		count := 0
		for _, row := range query(t, g, `MATCH (:Function {name:'Entry'})-[r:references]->(target:Field {name:'Name'}) RETURN r,target`, nil) {
			count++
			if row["target"].(Node).Location.Path != "end/type.go" {
				t.Fatalf("used the caller's import namespace: %+v", row)
			}
		}
		if count != want {
			t.Fatalf("name edges=%d want=%d", count, want)
		}
	}
	check(0)
	if err = g.AddDocuments(ctx, Document{Path: "end/type.go", Content: []byte("package end; type Leaf struct{Name int}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	check(2)
}

func TestGoMemberPropagationBudget(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "app.go", Content: []byte("package app;type Leaf struct{Value int};type Box struct{Item Leaf};func Entry(b Box){_=b.Item.Value}")}
	full, _, err := Build(ctx, "full", []Document{doc}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	limited, err := New("limited", Options{MaxRelations: len(full.Relations()) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(ctx); err == nil || len(limited.Nodes()) != 0 {
		t.Fatalf("propagation exceeded atomic relation budget: %v", err)
	}
}

func TestGoGenericMethodExpressionAcrossFiles(t *testing.T) {
	docs := []Document{
		{Path: "use.go", Content: []byte("package app; import lib \"example.org/lib\";func Entry(){_=Local[int].Work;_=lib.Generic[int].Work}")},
		{Path: "local.go", Content: []byte("package app;type Local[T any] struct{};func(Local[T]) Work(){}")},
		{Path: "lib/type.go", Content: []byte("package lib;type Generic[T any] struct{};func(Generic[T]) Work(){}")},
	}
	g, _, err := Build(context.Background(), "generic", docs, Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:references]->(target:Method {name:'Work'}) RETURN r,target`, nil)
	if len(rows) != 2 {
		t.Fatal(rows, g.Report())
	}
	for _, row := range rows {
		if row["r"].(Relation).Confidence != Scoped {
			t.Fatal(row)
		}
	}
}
