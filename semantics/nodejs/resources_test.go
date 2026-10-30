package nodejs_test

import (
	"context"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/codegraph/semantics/nodejs"
)

func TestURLReferencesDoNotDependOnReaderAPIs(t *testing.T) {
	for _, ext := range []string{"js", "ts", "tsx"} {
		for _, tc := range []struct {
			name, source string
			refs         int
			path         string
		}{
			{"standalone", `new URL('./asset.html',import.meta.url)`, 1, "src/asset.html"},
			{"arbitrary sink", `sendSomewhere(new URL('./asset.html',import.meta.url))`, 1, "src/asset.html"},
			{"import alias", `import {URL as Location} from 'node:url';new Location('./asset.html',import.meta.url)`, 1, "src/asset.html"},
			{"namespace", `import * as url from 'url';new url.URL('./asset.html',import.meta.url)`, 1, "src/asset.html"},
			{"shadowed", `function f(URL){return new URL('./asset.html',import.meta.url)}`, 0, ""},
			{"unknown module", `import {URL} from 'my-url';new URL('./asset.html',import.meta.url)`, 0, ""},
			{"bare string", `readFileSync('./asset.html')`, 0, ""},
			{"dynamic", `new URL(name,import.meta.url)`, 1, ""},
		} {
			t.Run(ext+"/"+tc.name, func(t *testing.T) {
				docs := []cg.Document{{Path: "src/a." + ext, Content: []byte(tc.source)}, {Path: "src/asset.html", Content: []byte("hello")}}
				for _, enabled := range []bool{false, true} {
					opts := cg.Options{}
					want := 0
					if enabled {
						opts.ResourceReferences = nodejs.ResourceReferences()
						want = tc.refs
					}
					g, _, err := cg.Build(context.Background(), "fixture", docs, opts)
					if err != nil {
						t.Fatal(err)
					}
					got := 0
					for _, n := range g.Nodes() {
						if n.Resource != nil {
							got++
							if n.ReferenceKind != cg.SymbolReference || n.Resource.Path != tc.path {
								t.Fatal(n)
							}
							edges := g.RelationsFrom(n.ID, cg.References)
							if (tc.path != "" && len(edges) != 1) || (tc.path == "" && len(edges) != 0) {
								t.Fatal(edges)
							}
						}
					}
					if got != want {
						t.Fatalf("enabled=%v: got %d want %d; report=%+v", enabled, got, want, g.Report())
					}
				}
			})
		}
	}
	rules := nodejs.ResourceReferences()
	rules[0].Name = "changed"
	if nodejs.ResourceReferences()[0].Name != "URL" {
		t.Fatal("shared mutable rule slice")
	}
}
