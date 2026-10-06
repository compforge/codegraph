package semantics_test

import (
	"context"
	cg "github.com/compforge/codegraph"
	"reflect"
	"strings"
	"testing"
)

func TestNamespaceBindingsAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		calls        int
		gap          string
	}{
		{"isolated", `namespace N { export function f() {} } function g(){ f(); }`, 0, "unresolved_call"},
		{"qualified", `namespace N { export function f() {} } function g(){ N.f(); }`, 1, ""},
		{"lexical", `namespace N { function f() {} export function g(){ f(); } }`, 1, ""},
		{"private", `namespace N { function f() {} } function g(){ N.f(); }`, 0, "dynamic_call"},
		{"shadow", `namespace N { export function f() {} } function g(N: any){ N.f(); }`, 0, "dynamic_call"},
		{"nested", `namespace N { export namespace M { export function f() {} } } function g(){ N.M.f(); }`, 1, ""},
		{"closure", `namespace N { export function f() {} } function g(){ return () => N.f(); }`, 0, "dynamic_call"},
		{"merged", `namespace N { export function f() {} } namespace N { export function g() {} } function h(){ N.f(); }`, 0, "unsupported_namespace_merge"},
		{"ambient", `declare module "pkg" { export function f(): void; } function g(){ f(); }`, 0, "unsupported_declaration"},
		{"dotted", `namespace N.M { export function f() {} } function g(){ f(); }`, 0, "unsupported_declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, report, err := cg.Build(context.Background(), "fixture", []cg.Document{{Path: "a.ts", Content: []byte(tc.source)}}, cg.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var calls []cg.Relation
			for _, r := range g.Relations() {
				if r.Kind == cg.Calls {
					calls = append(calls, r)
				}
			}
			if len(calls) != tc.calls {
				t.Fatalf("calls=%+v diagnostics=%+v", calls, report.Diagnostics)
			}
			if tc.gap != "" && !hasDiagnostic(report, tc.gap) {
				t.Fatal(report)
			}
			for _, call := range calls {
				if call.Confidence != cg.Exact {
					t.Fatal(call)
				}
				matched := false
				for _, r := range g.RelationsFrom(call.Source, cg.References) {
					if r.Target == call.Target && r.Location.StartByte >= call.Location.StartByte && r.Location.EndByte <= call.Location.EndByte {
						matched = true
					}
				}
				if !matched {
					t.Fatal("call has no corresponding lexical reference", call)
				}
			}
			if tc.name == "isolated" {
				child := g.Find("a.ts", cg.Function, "N.f")
				if len(child) != 1 {
					t.Fatal(g.Nodes())
				}
				parent := g.RelationsTo(child[0].ID, cg.Encloses)
				if len(parent) != 1 {
					t.Fatal(parent)
				}
				n, _ := g.Node(parent[0].Source)
				if n.Kind != cg.Namespace || n.Name != "N" {
					t.Fatal(n)
				}
			}
			if tc.name == "dotted" && len(g.Find("a.ts", cg.Function, "f")) != 0 {
				t.Fatal("unsupported owner promoted a child")
			}
		})
	}
}

func TestDeclarationMapping(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		kinds        map[string]cg.NodeKind
		gap          string
	}{
		{"a.go", "package p\nvar A, B int\nconst C, D = 1, 2\n", map[string]cg.NodeKind{"A": cg.Variable, "B": cg.Variable, "C": cg.Constant, "D": cg.Constant}, ""},
		{"a.ts", "class Box { value: number; run() {} } interface Shape { size: number; area(): number; }", map[string]cg.NodeKind{"Box": cg.Class, "Box.value": cg.Field, "Box.run": cg.Method, "Shape": cg.Interface, "Shape.size": cg.Property, "Shape.area": cg.Method}, ""},
		{"a.js", "class Box { value = 1; run() {} }", map[string]cg.NodeKind{"Box": cg.Class, "Box.value": cg.Field, "Box.run": cg.Method}, ""},
		{"a.py", "class Box:\n    def run(self):\n        pass\n", map[string]cg.NodeKind{"Box": cg.Class, "Box.run": cg.Method}, ""},
		{"a.ts", "const {a, b} = source; interface X { [key: string]: number }", map[string]cg.NodeKind{"X": cg.Interface}, "unsupported_declaration"},
	} {
		t.Run(tc.path+tc.source, func(t *testing.T) {
			g, report, err := cg.Build(context.Background(), "fixture", []cg.Document{{Path: tc.path, Content: []byte(tc.source)}}, cg.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if tc.gap != "" && !hasDiagnostic(report, tc.gap) {
				t.Fatal(report)
			}
			for name, kind := range tc.kinds {
				nodes := g.Find(tc.path, kind, name)
				if len(nodes) != 1 {
					t.Fatalf("missing %s/%s: %+v %+v", kind, name, g.Nodes(), report)
				}
				n := nodes[0]
				if n.NameLocation == nil || tc.source[n.NameLocation.StartByte:n.NameLocation.EndByte] != n.Name {
					t.Fatal(n)
				}
				if len(g.RelationsTo(n.ID, cg.Declares)) != 1 || len(g.RelationsTo(n.ID, cg.Encloses)) != 1 {
					t.Fatal(n, g.Relations())
				}
				if n.Signature == "" || n.SignatureLocation == nil {
					t.Fatal("missing signature", n)
				}
			}
			if tc.path == "a.go" {
				for _, n := range g.Find(tc.path, "", "") {
					edge := g.RelationsTo(n.ID, cg.Encloses)[0]
					owner, _ := g.Node(edge.Source)
					if owner.Kind != cg.DocumentNodeKind {
						t.Fatal("shared names became nested", n)
					}
				}
			}
		})
	}
}

func TestSignatureContractAndProjection(t *testing.T) {
	for _, tc := range []struct{ path, first, second, body string }{
		{"a.go", "package p\nfunc F(x int) int { return x }", "package p\nfunc F(x any) any { return x }", "package p\nfunc F(x int) int { return 0 }"},
		{"a.ts", "function F(x: number): number { return x; }", "function F(x: string): string { return x; }", "function F(x: number): number { return 0; }"},
		{"a.py", "def F(x: int) -> int:\n    return x\n", "def F(x: str) -> str:\n    return x\n", "def F(x: int) -> int:\n    return 0\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			build := func(source string) (*cg.Graph, cg.Node) {
				t.Helper()
				g, _, err := cg.Build(context.Background(), "fixture", []cg.Document{{Path: tc.path, Content: []byte(source)}}, cg.Options{})
				if err != nil {
					t.Fatal(err)
				}
				nodes := g.Find(tc.path, cg.Function, "F")
				if len(nodes) != 1 {
					t.Fatal(g.Nodes())
				}
				return g, nodes[0]
			}
			g, a := build(tc.first)
			_, b := build(tc.second)
			_, body := build(tc.body)
			if a.Signature == "" || a.Signature == b.Signature || a.Signature != body.Signature || strings.Contains(a.Signature, "return") {
				t.Fatal(a, b, body)
			}
			loc := a.SignatureLocation
			if loc == nil || tc.first[loc.StartByte:loc.EndByte] != a.Signature {
				t.Fatal(a)
			}
			rows := query(t, g, "MATCH (n:Function) RETURN n, n.signature AS signature, n.signatureStartByte AS start", nil)
			if len(rows) != 1 || !reflect.DeepEqual(rows[0]["n"], a) || rows[0]["signature"] != a.Signature || rows[0]["start"] != int64(loc.StartByte) {
				t.Fatal(rows)
			}
			a.SignatureLocation.StartByte = 999
			original, _ := g.Node(a.ID)
			if original.SignatureLocation.StartByte == 999 {
				t.Fatal("signature location aliases publication")
			}
		})
	}
}

func TestSignatureIncludesDeclarationModifiers(t *testing.T) {
	for _, tc := range []struct{ path, source, want string }{
		{"a.ts", "export async function f(x: number): Promise<number> { return x; }", "export async function f(x: number): Promise<number>"},
		{"a.py", "@decorate\ndef f(x: int) -> int:\n    return x\n", "@decorate\ndef f(x: int) -> int:"},
		{"A.java", "class A { public int f(int x) { return x; } }", "public int f(int x)"},
	} {
		g, _, err := cg.Build(context.Background(), "fixture", []cg.Document{{Path: tc.path, Content: []byte(tc.source)}}, cg.Options{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, n := range g.Find(tc.path, "", "") {
			if n.Name == "f" {
				found = n.Signature == tc.want
				if !found {
					t.Errorf("%s signature=%q want=%q", tc.path, n.Signature, tc.want)
				}
			}
		}
		if !found {
			t.Fatal(g.Nodes())
		}
	}
}
