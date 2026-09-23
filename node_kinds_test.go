package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
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
	g, report, err := Build(context.Background(), "rev", documents(source, "types.go"), Options{})
	if err != nil || !report.Complete {
		t.Fatal(report, err)
	}
	want := map[string]NodeKind{
		"User": Struct, "User.X": Field, "User.Y": Field, "User.Callback": Field, "User.Base": Field,
		"Other": Struct, "Other.X": Field, "Base": Struct,
		"Reader": Interface, "Reader.Read": Method, "OtherReader": Interface, "OtherReader.Close": Method,
		"ID": Type, "Alias": TypeAlias, "Literal": TypeAlias, "Literal.Value": Field,
		"Limit": Constant, "Current": Variable, "Work": Function, "User.Save": Method,
	}
	got := map[string]NodeKind{}
	for _, n := range g.Nodes() {
		if n.Kind != File {
			got[n.QualifiedName] = n.Kind
		}
	}
	if !reflect.DeepEqual(got, want) || len(g.Nodes()) != len(want)+1 {
		t.Fatalf("declarations=%v, want %v", got, want)
	}
	for _, row := range query(t, g, `MATCH (n) RETURN n, n.kind AS kind, labels(n) AS labels, properties(n) AS props`, nil) {
		n := row["n"].(Node)
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
		n := row["f"].(Node)
		if n.Name == "X" || n.Name == "Y" {
			if len(n.Markers) != 1 || n.Markers[0].Kind != Rule || n.Markers[0].Text != "Keep both coordinates" {
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
	if len(rows) != 1 || rows[0]["m"].(Node).QualifiedName != "Reader.Read" {
		t.Fatal("interface method ownership/marker", rows)
	}
	if len(query(t, g, `MATCH (:File)-[:contains]->(:Field) RETURN 1`, nil)) != 0 {
		t.Fatal("fields must be contained by their declaring type")
	}
	if len(query(t, g, `MATCH (:Struct {name:'User'})-[:contains]->(:Method {name:'Save'})-[:calls]->(:Function {name:'Work'}) RETURN 1`, nil)) != 1 {
		t.Fatal("method ownership/call path missing")
	}
	wantKinds := []NodeKind{Function, Method, Struct, Interface, Field, Type, TypeAlias, Variable, Constant}
	if !reflect.DeepEqual(Capabilities()[0].Declarations, wantKinds) {
		t.Fatal(Capabilities())
	}
	constants := query(t, g, `MATCH (n:Constant {name:'Limit'}) RETURN n`, nil)
	if len(constants) != 1 || len(constants[0]["n"].(Node).Markers) != 1 || constants[0]["n"].(Node).Markers[0].Kind != Doc {
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
	g, r, err := Build(ctx, "rev", documents(source, "methods.go"), Options{})
	if err != nil || r.Complete || !hasDiagnostic(r, "unresolved_receiver") {
		t.Fatal(r, err)
	}
	before := query(t, g, `MATCH (m:Method) RETURN m`, nil)[0]["m"].(Node)
	r, err = g.addDocumentsSync(ctx, documents(source, "types.go")...)
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	rows := query(t, g, `MATCH (b:Struct)-[r:contains]->(m:Method {name:'Run'}) RETURN b,r,m`, nil)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	edge, method := rows[0]["r"].(Relation), rows[0]["m"].(Node)
	if method.ID != before.ID || edge.Location != method.Location || edge.Location.Path != "methods.go" || edge.Confidence != Exact || edge.Basis != "receiver_declaration" {
		t.Fatal(edge, method)
	}
	if rows[0]["b"].(Node).QualifiedName != "Box" {
		t.Fatal("receiver resolved to a lexically nested type", rows)
	}
	if len(query(t, g, `MATCH (:Function {name:'Local'})-[:contains]->(:Struct {qualifiedName:'Local.Box'}) RETURN 1`, nil)) != 1 {
		t.Fatal("nested declaration ownership missing")
	}
	// File ownership and receiver ownership are distinct evidence, not inferred
	// from the physical location of the receiver's type declaration.
	if len(query(t, g, `MATCH (:File {path:'methods.go'})-[:contains]->(:Method) RETURN 1`, nil)) != 1 {
		t.Fatal("method's lexical file owner missing")
	}
	nodes, edges := g.Nodes(), g.Relations()
	if _, err = g.addDocumentsSync(ctx, documents(source, "methods.go", "types.go")...); err != nil || !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(edges, g.Relations()) {
		t.Fatal("repeated build changed concrete identities", err)
	}
}

func TestReceiverOwnershipUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name, targetPath, targetSource, code string
		count                                int
	}{
		{"duplicate", "b.go", "package p; type Box struct{}", "ambiguous_receiver", 2},
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
			g, r, err := Build(context.Background(), "rev", documents(source, "a.go", tc.targetPath), Options{})
			if err != nil || r.Complete || !hasDiagnostic(r, tc.code) {
				t.Fatal(r, err)
			}
			rows := query(t, g, `MATCH (:Struct)-[r:contains]->(:Method) RETURN r`, nil)
			if len(rows) != tc.count {
				t.Fatal(rows)
			}
			for _, row := range rows {
				if row["r"].(Relation).Confidence != Candidate {
					t.Fatal("ambiguous receiver marked exact", row)
				}
			}
		})
	}
}

func TestConcreteKindsBudgetRollback(t *testing.T) {
	source := fstest.MapFS{"types.go": {Data: []byte("package p; type Box struct{ X, Y int }; func (b Box) Run(){}")}}
	for _, opts := range []Options{{MaxNodes: 4}, {MaxRelations: 4}} {
		g, err := New("rev", opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = g.addDocumentsSync(context.Background(), documents(source, "types.go")...); !errors.Is(err, ErrBuildBudget) {
			t.Fatal("member/receiver growth bypassed budget", err)
		}
		if len(g.Nodes()) != 0 || len(g.Relations()) != 0 {
			t.Fatal("failed batch published")
		}
	}
}

func hasDiagnostic(r BuildReport, code string) bool {
	return slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == code })
}

func TestGroupedBlankFieldsHaveDistinctIdentities(t *testing.T) {
	source := fstest.MapFS{"types.go": {Data: []byte("package p; type Padding struct { _, _ int }")}}
	g, r, err := Build(context.Background(), "rev", documents(source, "types.go"), Options{})
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	rows := query(t, g, `MATCH (:Struct)-[:contains]->(f:Field) RETURN f`, nil)
	if len(rows) != 2 || rows[0]["f"].(Node).ID == rows[1]["f"].(Node).ID {
		t.Fatal("grouped blank fields collapsed", rows)
	}
	if len(query(t, g, `MATCH (:Field)-[:contains]->(:Field) RETURN 1`, nil)) != 0 {
		t.Fatal("overlapping field spans treated as nesting")
	}
}
