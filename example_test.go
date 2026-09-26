package codegraph_test

import (
	"context"
	"fmt"

	"github.com/compforge/codegraph"
)

func ExampleBuild() {
	documents := []codegraph.Document{{Path: "main.go", Content: []byte("package demo\nfunc Entry(){Work()}\nfunc Work(){}")}}
	g, report, err := codegraph.Build(context.Background(), "revision-1", documents, codegraph.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := g.Query(context.Background(), `MATCH (a:Function)-[:calls]->(b:Function {name:$name}) RETURN a.name AS caller`, map[string]any{"name": "Work"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(report.Diagnostics), rows[0]["caller"])
	// Output: 0 Entry
}

func ExampleGraph_AddDocuments() {
	g, err := codegraph.New("revision-1", codegraph.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}
	err = g.AddDocuments(context.Background(),
		codegraph.Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")},
		codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	report, err := g.Wait(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	work := g.Find("work.go", codegraph.Function, "Work")
	fmt.Println(len(report.Diagnostics), len(g.RelationsTo(work[0].ID, codegraph.Calls)))
	// Output: 0 1
}
