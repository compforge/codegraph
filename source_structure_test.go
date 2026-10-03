package codegraph

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

// +case=File outlines are derived from graph declarations and lexical edges, without reading source or parser outlines again.
func TestGraphSourceStructure(t *testing.T) {
	cases := []struct{ path, source, outline string }{
		{"box.go", "package p\r\n// 文档\r\ntype Box struct { A, B int; *Other }\r\ntype Other struct{}\r\ntype API interface { Run() }\r\nfunc (b Box) Run() {}\r\nfunc Outer() { type Inner struct { Value int }; _ = Inner{} }\r\n",
			"Struct Box\n  Field A\n  Field B\n  Field Other\nStruct Other\nInterface API\n  Method Run\nMethod Run\nFunction Outer\n  Struct Inner\n    Field Value\n"},
		{"box.py", "@decorate\nclass Box:\n    async def run(self):\n        def nested(): pass\n        return nested()\ndef outside(): pass\n",
			"Class Box\n  Method run\n    Function nested\nFunction outside\n"},
		{"box.ts", "export class Box { run() { function nested() {} } }\nfunction first() {} function second() {}\n",
			"Class Box\n  Method run\n    Function nested\nFunction first\nFunction second\n"},
		{"box.rs", "struct Box {}\nfn run() {}\n", "Struct Box\nFunction run\n"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			facts := extractTestFacts(t, newTestExtractor(t, nil), tc.path, tc.source)
			builder, err := NewBuilder("structure", Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err = builder.Add(facts); err != nil {
				t.Fatal(err)
			}
			g, _, err := builder.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := graphOutline(t, g, tc.path); got != tc.outline {
				t.Fatalf("outline =\n%s\nwant\n%s", got, tc.outline)
			}
			for _, d := range facts.Declarations {
				nodes := g.Find(tc.path, d.Kind, d.QualifiedName)
				if len(nodes) != 1 || !reflect.DeepEqual(nodes[0].NameLocation, d.NameLocation) {
					t.Fatalf("name location changed for %s: %+v", d.QualifiedName, nodes)
				}
				n := nodes[0]
				if n.NameLocation == nil {
					t.Fatalf("missing name location: %+v", n)
				}
				loc := *n.NameLocation
				if loc.StartByte < n.Location.StartByte || loc.EndByte > n.Location.EndByte || loc.StartByte >= loc.EndByte {
					t.Fatalf("name lies outside declaration: %+v", n)
				}
				// These fixtures use untransformed identifier captures.
				assertDocumentationLocation(t, tc.path, tc.source, Documentation{Text: n.Name, Location: loc})
				parents := g.RelationsTo(n.ID, Encloses)
				if len(parents) != 1 || parents[0].Confidence != Exact || parents[0].Location != *n.Location {
					t.Fatalf("declaration must have one lexical parent: %+v", parents)
				}
			}
			rows := query(t, g, `MATCH (:Document {path:$path})-[:declares]->(n) WHERE n.nameStartByte IS NOT NULL RETURN n, n.nameStartByte AS nameStart, n.nameEndByte AS nameEnd, n.nameLine AS line, n.nameColumn AS column, n.nameEndLine AS endLine, n.nameEndColumn AS endColumn, n.endLine AS declarationEndLine ORDER BY n.startByte`, map[string]any{"path": tc.path})
			if len(rows) != len(facts.Declarations) {
				t.Fatalf("missing Cypher names: %+v", rows)
			}
			for _, row := range rows {
				n := row["n"].(Node)
				loc := n.NameLocation
				for key, want := range map[string]int{"nameStart": loc.StartByte, "nameEnd": loc.EndByte, "line": loc.Line, "column": loc.Column, "endLine": loc.EndLine, "endColumn": loc.EndColumn, "declarationEndLine": n.Location.EndLine} {
					if row[key] != int64(want) {
						t.Fatalf("%s = %v, want %d", key, row[key], want)
					}
				}
			}
		})
	}
}

// This renderer intentionally knows only the public Graph contract. It also
// checks the equivalent Cypher view, so a second stored Outline cannot mask
// information missing from the graph.
func graphOutline(t *testing.T, g *Graph, path string) string {
	t.Helper()
	var out strings.Builder
	seen := map[string]bool{}
	var visit func(string, int)
	visit = func(parent string, depth int) {
		edges := g.RelationsFrom(parent, Encloses)
		rows := query(t, g, `MATCH (p {id:$parent})-[:encloses]->(c) RETURN c ORDER BY c.startByte`, map[string]any{"parent": parent})
		if len(edges) != len(rows) {
			t.Fatal("Cypher lost lexical children")
		}
		for i, edge := range edges {
			if seen[edge.Target] {
				t.Fatal("duplicate or cyclic lexical edge", edge)
			}
			seen[edge.Target] = true
			n, ok := g.Node(edge.Target)
			if !ok || n.Location == nil || n.Location.Path != path {
				t.Fatal("lexical edge leaves document", edge)
			}
			if !reflect.DeepEqual(n, rows[i]["c"]) {
				t.Fatal("Cypher outline differs", rows)
			}
			fmt.Fprintf(&out, "%s%s %s\n", strings.Repeat("  ", depth), n.Kind, n.Name)
			visit(n.ID, depth+1)
		}
	}
	visit(DocumentID(path), 0)
	if len(seen) != len(g.Find(path, "", "")) {
		t.Fatal("outline omitted a declaration")
	}
	return out.String()
}

// +case=Binding a receiver in another file enriches semantic membership without changing either file's lexical structure.
func TestSourceStructureIndependentOfBinding(t *testing.T) {
	ctx := context.Background()
	method := Document{Path: "methods.go", Content: []byte("package p\nfunc (b Box) Run() {}\n")}
	b, _ := NewBuilder("same-snapshot", Options{})
	f := extractTestFacts(t, newTestExtractor(t, nil), method.Path, string(method.Content))
	if err := b.Add(f); err != nil {
		t.Fatal(err)
	}
	before, _, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := before.Find(method.Path, Method, "Box.Run")[0]
	if len(before.RelationsTo(original.ID, Contains)) != 0 {
		t.Fatal("unresolved receiver was invented")
	}
	typ := extractTestFacts(t, newTestExtractor(t, nil), "types.go", "package p\ntype Box struct{}\n")
	if err := b.Add(typ); err != nil {
		t.Fatal(err)
	}
	after, _, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := graphOutline(t, after, method.Path); got != graphOutline(t, before, method.Path) {
		t.Fatal("binding changed lexical outline", got)
	}
	if !reflect.DeepEqual(before.RelationsTo(original.ID, Encloses), after.RelationsTo(original.ID, Encloses)) {
		t.Fatal("binding reparented lexical declaration")
	}
	owners := after.RelationsTo(original.ID, Contains)
	if len(owners) != 1 {
		t.Fatal("receiver ownership missing", owners)
	}
	owner, _ := after.Node(owners[0].Source)
	if owner.Name != "Box" || owner.Location.Path != "types.go" {
		t.Fatal("wrong semantic owner", owner)
	}
	if graphOutline(t, after, "types.go") != "Struct Box\n" {
		t.Fatal("method leaked into receiver file outline")
	}
	// Mutating an extraction view or a returned node must not move graph names.
	f.Declarations[0].NameLocation.StartByte = 0
	fresh, _ := f.View()
	if fresh.Declarations[0].NameLocation.StartByte == 0 {
		t.Fatal("Facts alias name location")
	}
	n := after.Find(method.Path, Method, "Box.Run")[0]
	n.NameLocation.StartByte = 0
	reloaded, _ := after.Node(original.ID)
	if reloaded.NameLocation.StartByte == 0 {
		t.Fatal("Find aliases graph name location")
	}
	rows := query(t, after, `MATCH (n:Method) RETURN n`, nil)
	rows[0]["n"].(Node).NameLocation.Path = "corrupt"
	if n, _ := after.Node(original.ID); n.NameLocation.Path != method.Path {
		t.Fatal("Query aliases graph name location")
	}
}

// +case=Cached extraction feeds graph production once; every outline view reads only published nodes and relations.
func TestGraphOutlineFromCachedProduction(t *testing.T) {
	ctx := context.Background()
	cache := extractionCache(t, 4, 4096)
	e := newTestExtractor(t, cache)
	parsed := 0
	parseObserver = func(string) { parsed++ }
	defer func() { parseObserver = nil }()
	doc := Document{Path: "box.ts", Content: []byte("export class Box { run() {} }\n")}
	facts, err := e.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	first := buildTestFacts(t, "first", Options{}, facts)
	again, err := e.Submit(ctx, doc).Wait()
	if err != nil {
		t.Fatal(err)
	}
	second := buildTestFacts(t, "second", Options{}, again)
	if parsed != 1 {
		t.Fatalf("parsed %d times, want one", parsed)
	}
	want := "Class Box\n  Method run\n"
	if graphOutline(t, first, doc.Path) != want || graphOutline(t, second, doc.Path) != want {
		t.Fatal("cached production changed graph structure")
	}
	doc.Content = []byte("export function changed() {}\n")
	changed, err := e.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	third := buildTestFacts(t, "third", Options{}, changed)
	if parsed != 2 || graphOutline(t, third, doc.Path) != "Function changed\n" {
		t.Fatal("changed content reused stale structure")
	}
	if graphOutline(t, first, doc.Path) != want {
		t.Fatal("later production changed a published outline")
	}
}

// +case=Consumers distinguish an empty file outline from unavailable analysis using the published coverage report.
func TestGraphOutlineCoverage(t *testing.T) {
	for _, tc := range []struct {
		doc        Document
		diagnostic string
	}{
		{Document{Path: "empty.go", Content: []byte("package p\n")}, ""},
		{Document{Path: "unknown.zzz", Content: []byte("some text")}, "unsupported_language"},
		{Document{Path: "sdk", Gitlink: strings.Repeat("a", 40)}, ""},
		{Document{Path: "data.json", Content: []byte(`{"x":1}`)}, "outline_incomplete"},
	} {
		t.Run(tc.doc.Path, func(t *testing.T) {
			g, report, err := Build(context.Background(), "coverage", []Document{tc.doc}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if graphOutline(t, g, tc.doc.Path) != "" {
				t.Fatal("invented declarations")
			}
			if _, ok := g.Node(tc.doc.ID()); !ok {
				t.Fatal("missing document")
			}
			if tc.diagnostic != "" && !hasDiagnostic(report, tc.diagnostic) {
				t.Fatal(report)
			}
			if tc.diagnostic == "" && len(report.Diagnostics) != 0 {
				t.Fatal(report)
			}
		})
	}
}

func TestInvalidOutlineQueryReportsGraphGap(t *testing.T) {
	entry := *grammars.DetectLanguageByName("python")
	entry.Name, entry.Extensions = "codegraph-structure-invalid-query", []string{".cgstructureinvalid"}
	entry.TagsQuery = `(node_that_does_not_exist) @definition.function`
	grammars.Register(entry)
	doc := Document{Path: "app.cgstructureinvalid", Content: []byte("def run():\n    pass\n")}
	g, report, err := Build(context.Background(), "invalid-query", []Document{doc}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(report, "outline_incomplete") || graphOutline(t, g, doc.Path) != "" {
		t.Fatalf("query gap missing from published graph report: %+v", report)
	}
}
