package semantics_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/compforge/codegraph"
)

// +link=https://github.com/odvcencio/gotreesitter/pull/1291
// +case=`Long C statement lists preserve complete function declarations through strict parsing`
func TestCLongStatementListDeclarations(t *testing.T) {
	// Cross the parser's former 4096-depth conflict cutoff and verify that
	// the declaration spans the complete body, rather than a partial tree.
	var source strings.Builder
	source.WriteString("void entry(void) {\n")
	for i := range 8192 {
		fmt.Fprintf(&source, "emit(\"x\", %d);\n", i)
	}
	source.WriteString("}\n")
	entryEnd := source.Len() - 1
	g, report, err := codegraph.Build(context.Background(), "long-c", []codegraph.Document{
		{Path: "generated.c", Content: []byte(source.String())},
	}, codegraph.Options{ParseTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// C currently has declaration extraction only. Keep that coverage gap
	// separate from the parser correctness this case exercises.
	if !hasDiagnostic(report, "unsupported_resolution") {
		t.Fatal(report)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code != "unsupported_resolution" {
			t.Fatalf("unexpected diagnostic: %+v", diagnostic)
		}
	}
	functions := g.Find("generated.c", codegraph.Function, "")
	if len(functions) != 1 {
		t.Fatalf("got %d functions, want entry", len(functions))
	}
	got := functions[0]
	if got.Name != "entry" || got.Location.StartByte != 0 || got.Location.EndByte != entryEnd {
		t.Fatalf("incomplete function: %+v, location=%+v; want span 0..%d", got, got.Location, entryEnd)
	}
}
