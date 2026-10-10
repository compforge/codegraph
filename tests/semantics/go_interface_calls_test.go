package semantics_test

import (
	"context"
	"testing"

	"github.com/compforge/codegraph"
)

// +spec=An observed concrete argument to an interface parameter supplies heuristic dispatch candidates across packages.
func TestGoInterfaceArgumentCalls(t *testing.T) {
	for _, tc := range []struct {
		name, argument, setupPath string
		want                      bool
	}{
		{"value", "source{}", "cmd/main.go", true},
		{"pointer", "&source{}", "cmd/main.go", true},
		{"no-injection", "nil", "cmd/main.go", false},
		{"variable-not-yet-tracked", "instance", "cmd/main.go", false},
		{"test-only-injection", "source{}", "cmd/main_test.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs := []codegraph.Document{
				{Path: "go.mod", Content: []byte("module example.org/app\n")},
				{Path: "observer/observer.go", Content: []byte(`package observer
 type Source interface { Observe(string) string }
 type OtherSource interface { Observe(string) string }
 func Other(source OtherSource) {}
 type Observer struct { source Source }
 func New(unused int, source Source) *Observer { return &Observer{source: source} }
 func (o *Observer) Poll() string { return o.source.Observe("bed") }
`)},
				{Path: "cmd/types.go", Content: []byte(`package main
 var instance = source{}
 type source struct{}
 func (source) Observe(s string) string { return s }
 type unrelated struct{}
 func (unrelated) Observe(s string) string { return s }
`)},
				{Path: tc.setupPath, Content: []byte(`package main
 import "example.org/app/observer"
 func setup() { observer.Other(unrelated{}); observer.New(0, ` + tc.argument + `).Poll() }
`)},
			}
			g, _, err := codegraph.Build(context.Background(), "interface-argument", docs, codegraph.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rows := query(t, g, `MATCH (source)-[r:calls]->(target) WHERE source.qualifiedName = 'Observer.Poll' RETURN target,r`, nil)
			found, contract := false, false
			for _, row := range rows {
				target, edge := row["target"].(codegraph.Node), row["r"].(codegraph.Relation)
				if target.QualifiedName == "Source.Observe" {
					contract = true
				}
				if target.QualifiedName == "unrelated.Observe" {
					t.Fatal("unobserved implementation became a call target")
				}
				if target.QualifiedName == "source.Observe" {
					found = true
					if edge.Evidence[0].Location == nil || edge.Evidence[0].Location.Path != tc.setupPath {
						t.Fatalf("missing injection location: %+v", edge)
					}
					if edge.Confidence != codegraph.Heuristic || len(edge.Evidence) != 1 || edge.Evidence[0].Basis != "interface_argument" {
						t.Fatalf("unexpected evidence: %+v", edge)
					}
				}
			}
			if !contract || found != tc.want {
				t.Fatalf("contract=%v implementation=%v want=%v; rows=%+v", contract, found, tc.want, rows)
			}
		})
	}
}
