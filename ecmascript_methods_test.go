package codegraph

import (
	"context"
	"strings"
	"testing"
)

// +case=`Generator functions and private methods retain concrete kinds, names, full ranges and enclosing owners`
func TestECMAScriptGeneratorAndPrivateDeclarations(t *testing.T) {
	for _, path := range []string{"app.js", "app.ts", "app.tsx"} {
		t.Run(path, func(t *testing.T) {
			methods := []string{
				"#hidden() { return 1; }",
				"async #pending() { return 1; }",
				"*iterate() { yield 1; }",
				"async *events() { yield 1; }",
				"async() { return 1; }",
				"get size() { return 1; }",
				"set size(value) {}",
				"commented /* name docs */ () {}",
			}
			names := []string{"#hidden", "#pending", "iterate", "events", "async", "size", "size", "commented"}
			if path != "app.js" {
				methods = append(methods,
					"private async *streamWithRetry(value: string): AsyncGenerator<string> { yield value; }",
					"protected async *generic /* type docs */ <T>(value: T): AsyncGenerator<T> { yield value; }",
					"public async ordinary<T>(value: T): Promise<T> { return value; }")
				names = append(names, "streamWithRetry", "generic", "ordinary")
			}
			generators := []string{"function* syncItems() { yield 1; }", "async function* asyncItems() { yield 1; }"}
			source := "// +spec=Stream values\nexport " + generators[0] + "\nexport " + generators[1] + "\nclass Box {\n" + strings.Join(methods, "\n") + "\n}"
			g, report, err := Build(context.Background(), "callables", []Document{{Path: path, Content: []byte(source)}}, Options{})
			if err != nil || hasDiagnostic(report, "outline_incomplete") {
				t.Fatal(report, err)
			}
			facts, err := g.Extract(context.Background(), Document{Path: path, Content: []byte(source)})
			if err != nil {
				t.Fatal(err)
			}
			expected := map[int]struct {
				name, span string
				kind       NodeKind
			}{}
			for i, method := range methods {
				expected[strings.Index(source, method)] = struct {
					name, span string
					kind       NodeKind
				}{names[i], method, Method}
			}
			for i, generator := range generators {
				expected[strings.Index(source, generator)] = struct {
					name, span string
					kind       NodeKind
				}{[]string{"syncItems", "asyncItems"}[i], generator, Function}
			}
			for _, node := range g.Nodes() {
				if node.Kind != Method && node.Kind != Function {
					continue
				}
				if node.Location == nil {
					t.Fatal(node)
				}
				want, ok := expected[node.Location.StartByte]
				if !ok || node.Name != want.name || node.Kind != want.kind || source[node.Location.StartByte:node.Location.EndByte] != want.span {
					t.Fatalf("unexpected callable: %+v, expected %+v", node, want)
				}
				delete(expected, node.Location.StartByte)
				if node.Kind == Method {
					edges := g.RelationsTo(node.ID, Contains)
					if len(edges) != 1 {
						t.Fatal("method owner", node, edges)
					}
					owner, ok := g.Node(edges[0].Source)
					if !ok || owner.Name != "Box" {
						t.Fatal("wrong owner", owner)
					}
				}
				if node.Name == "syncItems" && (len(node.Markers) != 1 || node.Markers[0].Text != "Stream values") {
					t.Fatal("missing generator marker", node)
				}
				nameStart := node.Location.StartByte + strings.Index(want.span, want.name)
				for _, ref := range facts.References {
					if ref.Location.StartByte == nameStart {
						t.Fatal("declaration emitted as reference", ref)
					}
				}
			}
			if len(expected) != 0 {
				t.Fatalf("missing callables: %v", expected)
			}
			for _, ref := range facts.References {
				if ref.Name == "async" {
					t.Fatal("modifier emitted as reference", ref)
				}
			}
		})
	}
}
