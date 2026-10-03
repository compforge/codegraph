package semantics_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/compforge/codegraph"
)

func TestConcreteNodeKinds(t *testing.T) {
	source := fstest.MapFS{"types.go": {Data: []byte(`package p
// +spec=User contract
type User struct {
 // +rule=Keep both coordinates
 X, Y int
 Callback func()
 *Base[int]
}
type Other struct { X string }
type Base[T any] struct{}
type Reader interface {
 // +doc=Read contract
 Read() error
 OtherReader
}
type OtherReader interface { Close() error }
type ID int
type Alias = User
type Literal = struct { Value int }
// +doc=Default limit
const Limit = 10
var Current = Limit
func Work(){}
func (u *User) Save(){ Work() }
`)}}
	g, report, err := codegraph.Build(context.Background(), "rev", documents(source, "types.go"), codegraph.Options{})
	if err != nil || len(report.Diagnostics) != 0 {
		t.Fatal(report, err)
	}
	want := map[string]codegraph.NodeKind{
		"User": codegraph.Struct, "User.X": codegraph.Field, "User.Y": codegraph.Field, "User.Callback": codegraph.Field, "User.Base": codegraph.Field,
		"Other": codegraph.Struct, "Other.X": codegraph.Field, "Base": codegraph.Struct,
		"Reader": codegraph.Interface, "Reader.Read": codegraph.Method, "OtherReader": codegraph.Interface, "OtherReader.Close": codegraph.Method,
		"ID": codegraph.Type, "Alias": codegraph.TypeAlias, "Literal": codegraph.TypeAlias, "Literal.Value": codegraph.Field,
		"Limit": codegraph.Constant, "Current": codegraph.Variable, "Work": codegraph.Function, "User.Save": codegraph.Method,
	}
	got := map[string]codegraph.NodeKind{}
	for _, n := range g.Find("types.go", "", "") {
		got[n.QualifiedName] = n.Kind
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declarations=%v, want %v", got, want)
	}
	for _, row := range query(t, g, `MATCH (n) RETURN n, n.kind AS kind, labels(n) AS labels, properties(n) AS props`, nil) {
		n := row["n"].(codegraph.Node)
		if row["kind"] != string(n.Kind) || !reflect.DeepEqual(row["labels"], []any{string(n.Kind)}) {
			t.Fatal("node kind/label mismatch", row)
		}
		if _, exists := row["props"].(map[string]any)["symbolKind"]; exists {
			t.Fatal("symbolKind leaked into graph properties")
		}
		data, err := json.Marshal(n)
		if err != nil || strings.Contains(string(data), "symbolKind") || strings.HasPrefix(n.ID, "symbol:") {
			t.Fatal("symbol category leaked into node", string(data), err)
		}
	}
	if rows := query(t, g, `MATCH (n:Symbol) RETURN n`, nil); len(rows) != 0 {
		t.Fatal("symbol label present", rows)
	}
	fields := query(t, g, `MATCH (:Struct {name:'User'})-[:contains]->(f:Field) RETURN f`, nil)
	if len(fields) != 4 {
		t.Fatal(fields)
	}
	for _, row := range fields {
		n := row["f"].(codegraph.Node)
		if n.Name == "X" || n.Name == "Y" {
			if len(n.Markers) != 1 || n.Markers[0].Kind != codegraph.Rule || n.Markers[0].Text != "Keep both coordinates" {
				t.Fatal("field marker lost", n)
			}
			wantSpan := "X, Y int"
			if n.Name == "Y" {
				wantSpan = "Y int"
			}
			if string(source["types.go"].Data[n.Location.StartByte:n.Location.EndByte]) != wantSpan {
				t.Fatal("field span lost", n)
			}
		}
	}
	rows := query(t, g, `MATCH (:Interface {name:'Reader'})-[:contains]->(m:Method) WHERE 'doc' IN m.markers RETURN m`, nil)
	if len(rows) != 1 || rows[0]["m"].(codegraph.Node).QualifiedName != "Reader.Read" {
		t.Fatal("interface method ownership/marker", rows)
	}
	if len(query(t, g, `MATCH (:Document)-[:contains]->(:Field) RETURN 1`, nil)) != 0 {
		t.Fatal("fields must be contained by their declaring type")
	}
	if len(query(t, g, `MATCH (:Struct {name:'User'})-[:contains]->(:Method {name:'Save'})-[:calls]->(:Function {name:'Work'}) RETURN 1`, nil)) != 1 {
		t.Fatal("method ownership/call path missing")
	}
	wantKinds := []codegraph.NodeKind{codegraph.Function, codegraph.Method, codegraph.Struct, codegraph.Interface, codegraph.Field, codegraph.Type, codegraph.TypeAlias, codegraph.Variable, codegraph.Constant}
	if !reflect.DeepEqual(codegraph.Capabilities()[0].Declarations, wantKinds) {
		t.Fatal(codegraph.Capabilities())
	}
	constants := query(t, g, `MATCH (n:Constant {name:'Limit'}) RETURN n`, nil)
	if len(constants) != 1 || len(constants[0]["n"].(codegraph.Node).Markers) != 1 || constants[0]["n"].(codegraph.Node).Markers[0].Kind != codegraph.Doc {
		t.Fatal("constant marker lost", constants)
	}
}

func TestReceiverOwnershipAcrossBatches(t *testing.T) {
	ctx := context.Background()
	source := fstest.MapFS{
		"types.go": {Data: []byte("package p\ntype Box[T any] struct { Value T }")},
		"methods.go": {Data: []byte(`package p

// +case=Cross-file receiver
func (b *Box[T]) Run(){ Work() }
func Work(){}
func Local(){ type Box struct{} }
`)},
	}
	g, r, err := buildTestBuilder(ctx, "rev", documents(source, "methods.go"), codegraph.Options{})
	if err != nil || (len(r.Diagnostics) == 0) || !hasDiagnostic(r, "unresolved_receiver") {
		t.Fatal(r, err)
	}
	before := query(t, g.Result(), `MATCH (m:Method) RETURN m`, nil)[0]["m"].(codegraph.Node)
	r, err = addDocumentsSync(g, ctx, documents(source, "types.go")...)
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	rows := query(t, g.Result(), `MATCH (b:Struct)-[r:contains]->(m:Method {name:'Run'}) RETURN b,r,m`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	edge, method := rows[0]["r"].(codegraph.Relation), rows[0]["m"].(codegraph.Node)
	if method.ID != before.ID || edge.Location != *method.Location || edge.Location.Path != "methods.go" || edge.Confidence != codegraph.Exact || edge.Evidence[0].Basis != "receiver_declaration" {
		t.Fatal(edge, method)
	}
	if rows[0]["b"].(codegraph.Node).QualifiedName != "Box" {
		t.Fatal("receiver resolved to a lexically nested type", rows)
	}
	if len(query(t, g.Result(), `MATCH (:Function {name:'Local'})-[:contains]->(:Struct {qualifiedName:'Local.Box'}) RETURN 1`, nil)) != 1 {
		t.Fatal("nested declaration ownership missing")
	}
	// Document ownership and receiver ownership are distinct evidence, not inferred
	// from the physical location of the receiver's type declaration.
	if len(query(t, g.Result(), `MATCH (:Document {path:'methods.go'})-[:declares]->(:Method) RETURN 1`, nil)) != 1 {
		t.Fatal("method's lexical file owner missing")
	}
	nodes, edges := g.Result().Nodes(), g.Result().Relations()
	if _, err = addDocumentsSync(g, ctx, documents(source, "methods.go", "types.go")...); err != nil || !reflect.DeepEqual(nodes, g.Result().Nodes()) || !reflect.DeepEqual(edges, g.Result().Relations()) {
		t.Fatal("repeated build changed concrete identities", err)
	}
}

func TestReceiverOwnershipUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name, targetPath, targetSource, code string
		count                                int
	}{
		{"duplicate", "b.go", "package p; type Box struct{}", "", 2},
		{"test_only", "b_test.go", "package p; type Missing struct{}", "unresolved_receiver", 0},
		{"other_package", "b.go", "package other; type Missing struct{}", "unresolved_receiver", 0},
		{"other_directory", "other/b.go", "package p; type Missing struct{}", "unresolved_receiver", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver := "Missing"
			if tc.count == 2 {
				receiver = "Box"
			}
			source := fstest.MapFS{
				"a.go":        {Data: []byte("package p; type Box struct{}; func (b *" + receiver + ") Run(){}")},
				tc.targetPath: {Data: []byte(tc.targetSource)},
			}
			g, r, err := codegraph.Build(context.Background(), "rev", documents(source, "a.go", tc.targetPath), codegraph.Options{})
			if err != nil || (tc.code == "" && len(r.Diagnostics) != 0) || (tc.code != "" && !hasDiagnostic(r, tc.code)) {
				t.Fatal(r, err)
			}
			rows := query(t, g, `MATCH (:Struct)-[r:contains]->(:Method) RETURN r`, nil)
			if len(rows) != tc.count {
				t.Fatal(rows)
			}
			for _, row := range rows {
				if row["r"].(codegraph.Relation).Confidence != codegraph.Scoped {
					t.Fatal("ambiguous receiver marked exact", row)
				}
			}
		})
	}
}

func TestConcreteKindsBudgetRollback(t *testing.T) {
	source := fstest.MapFS{"types.go": {Data: []byte("package p; type Box struct{ X, Y int }; func (b Box) Run(){}")}}
	for _, opts := range []codegraph.Options{{MaxNodes: 4}, {MaxRelations: 4}} {
		g, err := codegraph.NewBuilder("rev", opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = addDocumentsSync(g, context.Background(), documents(source, "types.go")...); !errors.Is(err, codegraph.ErrBuildBudget) {
			t.Fatal("member/receiver growth bypassed budget", err)
		}
		if len(g.Result().Nodes()) != 0 || len(g.Result().Relations()) != 0 {
			t.Fatal("failed batch published")
		}
	}
}

func hasDiagnostic(r codegraph.BuildReport, code string) bool {
	return slices.ContainsFunc(r.Diagnostics, func(d codegraph.Diagnostic) bool { return d.Code == code })
}

func TestGroupedBlankFieldsHaveDistinctIdentities(t *testing.T) {
	source := fstest.MapFS{"types.go": {Data: []byte("package p; type Padding struct { _, _ int }")}}
	g, r, err := codegraph.Build(context.Background(), "rev", documents(source, "types.go"), codegraph.Options{})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatal(r, err)
	}
	rows := query(t, g, `MATCH (:Struct)-[:contains]->(f:Field) RETURN f`, nil)
	if len(rows) != 2 || rows[0]["f"].(codegraph.Node).ID == rows[1]["f"].(codegraph.Node).ID {
		t.Fatal("grouped blank fields collapsed", rows)
	}
	if len(query(t, g, `MATCH (:Field)-[:contains]->(:Field) RETURN 1`, nil)) != 0 {
		t.Fatal("overlapping field spans treated as nesting")
	}
}
