package semantics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

// +link=https://github.com/odvcencio/gotreesitter/pull/1561
// +case:id=make-builtin-variable,expect=`Builtin-named variables remain valid in nested Make expressions`
func TestMakeBuiltinVariableReferences(t *testing.T) {
	extractor, err := codegraph.NewExtractor(codegraph.ExtractionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expression := range []string{"$(file)", "${dir}", "$(foreach file,a b,$(file))", "$(dir src/a.go)"} {
		t.Run(expression, func(t *testing.T) {
			facts, err := extractor.Extract(context.Background(), codegraph.Document{Path: "Makefile", Content: []byte("FILES := " + expression + "\nall:\n\t@echo $(FILES)\n")})
			if err != nil {
				t.Fatal(err)
			}
			if facts.Language != "make" {
				t.Fatalf("language = %q", facts.Language)
			}
		})
	}
}

// +link=https://github.com/odvcencio/gotreesitter/pull/1483
// +link=https://github.com/odvcencio/gotreesitter/pull/1515
// +case:id=typescript-generic-expression,expect=`Generic arrows and tagged templates preserve following declarations`
func TestTypeScriptGenericExpressions(t *testing.T) {
	for _, path := range []string{"app.ts", "app.tsx"} {
		for _, expression := range []string{
			"const identity = async <T>(v: T): Promise<T> => { const local = v; return local; };\n",
			"const result = tag<string[]>`text`;\n",
		} {
			t.Run(path+"/"+expression, func(t *testing.T) {
				source := expression + "export function after() { return 1; }\n"
				g, report, err := codegraph.Build(context.Background(), "generic", []codegraph.Document{{Path: path, Content: []byte(source)}}, codegraph.Options{})
				if err != nil {
					t.Fatal(err)
				}
				// Unbound generic parameters and external names are existing semantic
				// gaps; they must not hide parser or outline failures.
				for _, diagnostic := range report.Diagnostics {
					if diagnostic.Code != "unresolved_reference" && diagnostic.Code != "unresolved_call" {
						t.Fatalf("unexpected diagnostic: %+v", diagnostic)
					}
				}
				if strings.Contains(expression, "identity") {
					if len(g.Find(path, codegraph.Variable, "identity.local")) != 1 {
						t.Fatal("generic arrow lost its local declaration owner")
					}
				} else {
					found := false
					for _, node := range g.Nodes() {
						if node.Kind == codegraph.CallSite && node.Name == "tag" {
							found = true
						}
					}
					if !found {
						t.Fatal("generic tagged template lost its call occurrence")
					}
				}
				functions := g.Find(path, codegraph.Function, "after")
				if len(functions) != 1 {
					t.Fatalf("missing following declaration: %+v", functions)
				}
				loc := functions[0].Location
				if loc == nil || source[loc.StartByte:loc.EndByte] != "function after() { return 1; }" {
					t.Fatalf("wrong declaration span: %+v", loc)
				}
			})
		}
	}
}
