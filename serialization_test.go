package codegraph_test

import (
	"encoding/json"
	"testing"

	"github.com/compforge/codegraph"
)

// Public values keep their JSON field names when implementation packages move.
func TestSourceLocationJSONContract(t *testing.T) {
	loc := codegraph.Location{Path: "a.go", StartByte: 3, EndByte: 7, Line: 1, Column: 4, EndLine: 1, EndColumn: 8}
	for _, value := range []any{
		codegraph.Node{Location: &loc}, codegraph.Relation{Location: loc},
		codegraph.Evidence{Location: &loc}, codegraph.Marker{Location: loc},
		codegraph.Diagnostic{Location: loc},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		var got codegraph.Location
		if err := json.Unmarshal(fields["location"], &got); err != nil || got != loc {
			t.Fatalf("%T source location changed: %s (%v)", value, data, err)
		}
		if _, leaked := fields["SourceLocation"]; leaked {
			t.Fatalf("implementation name escaped: %s", data)
		}
	}
}
