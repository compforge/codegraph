package codegraph_test

import (
	"context"
	"fmt"
	"testing/fstest"

	"github.com/compforge/codegraph"
)

func ExampleBuild() {
	source := fstest.MapFS{"main.go": {Data: []byte("package demo\nfunc Entry(){Work()}\nfunc Work(){}")}}
	g, report, err := codegraph.Build(context.Background(), "revision-1", source, []string{"main.go"}, codegraph.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := g.Query(context.Background(), `MATCH (a:Function)-[:calls]->(b:Function {name:$name}) RETURN a.name AS caller`, map[string]any{"name": "Work"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(report.Complete, rows[0]["caller"])
	// Output: true Entry
}
