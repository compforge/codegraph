package semantics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

func TestGoDeclaredResultTypes(t *testing.T) {
	for _, tc := range []struct{ name, setup, expr, target string }{
		{"function", "", "Make().Name", "Leaf.Name"},
		{"assigned", "item:=Make();", "item.Name", "Leaf.Name"},
		{"method", "", "box.Make().Name", "Leaf.Name"},
		{"interface", "", "api.Make().Name", "Leaf.Name"},
		{"function-field", "", "box.Factory().Name", "Leaf.Name"},
		{"function-alias", "factory:=Make;", "factory().Name", "Leaf.Name"},
		{"method-value", "factory:=box.Make;", "factory().Name", "Leaf.Name"},
		{"typed-callback", "", "callback().Name", "Leaf.Name"},
		{"shadowed-function", "var Make func()Other;", "Make().Name", "Other.Name"},
		{"named-callback", "var factory Factory;", "factory().Name", "Leaf.Name"},
		{"asserted-callback", "factory:=unknown.(func() Leaf);", "factory().Name", "Leaf.Name"},
		{"literal", "", "func() Leaf{return Leaf{}}().Name", "Leaf.Name"},
		{"returned-callback", "", "Provider()().Name", "Leaf.Name"},
		{"tuple-first", "first,second:=Pair();_=second;", "first.Name", "Other.Name"},
		{"tuple-second", "first,second:=Pair();_=first;", "second.Name", "Leaf.Name"},
		{"blank-first", "_,second:=Pair();", "second.Name", "Leaf.Name"},
		{"var-tuple", "var first,second=Pair();_=first;", "second.Name", "Leaf.Name"},
		{"separate-rhs", "first,second:=OtherMake(),Make();_=first;", "second.Name", "Leaf.Name"},
		{"grouped-results", "_,second:=Grouped();", "second.Name", "Leaf.Name"},
		{"tuple-reassign", "first,second:=Pair();first,second=Pair();_=first;", "second.Name", "Leaf.Name"},
		{"method-tuple", "_,second:=box.Pair();", "second.Name", "Leaf.Name"},
		{"result-container", "", "List()[0].Name", "Leaf.Name"},
		{"result-member", "", "NewBox().Child.Name", "Leaf.Name"},
		{"recursive-function", "", "Recursive().Name", "Leaf.Name"},
		{"conversion", "", "Leaf(Leaf{}).Name", "Leaf.Name"},
		{"unknown-function", "", "Missing().Name", ""},
		{"generic-substitution-gap", "", "Identity[Leaf](Leaf{}).Name", ""},
		{"missing-slot", "_,second:=Make();", "second.Name", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `package app
 const Name=1
 type Leaf struct{Name int}
 type Other struct{Name string}
 type Box struct{Child Leaf;Factory func()Leaf}
 type API interface{Make() Leaf}
 type Factory func()Leaf
 func Make()Leaf{return Leaf{}}
 func OtherMake()Other{return Other{}}
 func Pair()(Other,Leaf){return Other{},Leaf{}}
 func Grouped()(a,b Leaf){return}
 func List()[]Leaf{return nil}
 func NewBox()*Box{return nil}
 func Recursive()Leaf{return Recursive()}
 func Provider()func()Leaf{return Make}
 func Identity[T any](x T)T{return x}
 func(b Box)Make()Leaf{return Leaf{}}
 func(b Box)Pair()(Other,Leaf){return Pair()}
 func Entry(box Box,api API,callback func()Leaf,unknown any){ ` + tc.setup + ` _ = ` + tc.expr + ` }
 `
			g, _, err := codegraph.Build(context.Background(), "results", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			start := strings.LastIndex(source, tc.expr) + strings.LastIndex(tc.expr, ".") + 1
			count := 0
			for _, row := range query(t, g, `MATCH (source)-[r:references]->(target) WHERE source.kind <> 'Reference' RETURN r,target`, nil) {
				e, n := row["r"].(codegraph.Relation), row["target"].(codegraph.Node)
				if e.Location.StartByte != start {
					continue
				}
				count++
				if n.QualifiedName != tc.target || e.Confidence != codegraph.Scoped {
					t.Fatalf("wrong result binding: %+v", row)
				}
			}
			if tc.target != "" {
				if count != 1 {
					t.Fatalf("result edges=%d: %+v", count, g.Report())
				}
			} else {
				if count != 0 {
					t.Fatal("unknown result guessed a target")
				}
				gap := false
				for _, d := range g.Report().Diagnostics {
					if d.Code == "unresolved_reference" && d.Location.StartByte == start {
						gap = true
					}
				}
				if !gap {
					t.Fatal("unknown result lost local diagnostic")
				}
			}
		})
	}
}

func TestGoResultTypeCallTargets(t *testing.T) {
	source := `package app
 type Leaf struct{}
 func(Leaf)Run(){}
 type Other struct{}
 func(Other)Run(){}
 type Box struct{Child Leaf; Factory func()Leaf}
 func Make()Leaf{return Leaf{}}
 func Pair()(Other,Leaf){return Other{},Leaf{}}
 func Entry(box Box){Make().Run();_,item:=Pair();item.Run();box.Child.Run();box.Factory().Run()}
 `
	g, _, err := codegraph.Build(context.Background(), "result-calls", []codegraph.Document{{Path: "app.go", Content: []byte(source)}}, codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := query(t, g, `MATCH (:Function {name:'Entry'})-[r:calls]->(target:Method {name:'Run'}) RETURN r,target`, nil)
	if len(rows) != 4 {
		t.Fatal(rows, g.Report())
	}
	for _, row := range rows {
		if row["target"].(codegraph.Node).QualifiedName != "Leaf.Run" || row["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
			t.Fatal(row)
		}
	}
	limited, err := codegraph.NewBuilder("result-budget", codegraph.Options{MaxRelations: len(g.Relations()) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = limited.AddDocuments(context.Background(), codegraph.Document{Path: "app.go", Content: []byte(source)}); err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Wait(context.Background()); err == nil || len(limited.Result().Nodes()) != 0 {
		t.Fatalf("result propagation must preserve atomic budget: %v", err)
	}
}

func TestGoResultTypeDeclarationContext(t *testing.T) {
	ctx := context.Background()
	source := `package app;import lib "example.org/factory";import types "example.org/decoy";var decoy types.Leaf;func Entry(){_=lib.Make().Name}`
	g, _, err := buildTestBuilder(ctx, "context", []codegraph.Document{
		{Path: "app.go", Content: []byte(source)},
		{Path: "factory/make.go", Content: []byte("package factory;import types \"example.org/model\";func Make()*types.Leaf{return nil}")},
		{Path: "decoy/type.go", Content: []byte("package decoy;type Leaf struct{Name string}")},
	}, codegraph.Options{ModulePath: "example.org"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		rows := query(t, g.Result(), `MATCH (:Function {name:'Entry'})-[r:references]->(target:Field {name:'Name'}) RETURN target`, nil)
		if len(rows) != want {
			t.Fatal(rows)
		}
		for _, row := range rows {
			if row["target"].(codegraph.Node).Location.Path != "model/type.go" {
				t.Fatal(row)
			}
		}
	}
	check(0)
	if err = g.AddDocuments(ctx, codegraph.Document{Path: "model/type.go", Content: []byte("package model;type Leaf struct{Name int}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	check(1)
}
