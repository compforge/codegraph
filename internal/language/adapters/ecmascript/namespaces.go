package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"strings"
)

// Qualified namespace access is a lexical binding followed by explicit exports.
// It is never a global search for a same-spelled declaration.
func bindNamespaceUses(f *Facts, tree *gts.Tree) {
	exported := map[int]bool{}
	var closures []analysis.Span
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(tree.Language()) {
		case "arrow_function", "function_expression", "generator_function":
			closures = append(closures, syntax.NodeSpan(n))
		case "export_statement":
			if child := n.ChildByFieldName("declaration", tree.Language()); child != nil {
				for i, d := range f.Declarations {
					if d.Start == int(child.StartByte()) {
						exported[i] = true
					}
				}
			}
		}
	})
	member := func(owner int, name string) int {
		target := -1
		for i, d := range f.Declarations {
			if d.Parent == owner && d.Name == name && exported[i] {
				if target >= 0 {
					return -1
				}
				target = i
			}
		}
		return target
	}
	for i := range f.References {
		r := &f.References[i]
		if r.Receiver == "" {
			continue
		}
		parts := strings.Split(r.Receiver, ".")
		bindings := f.Lexical.Lookup(parts[0], r.Span)
		if len(bindings) != 1 || bindings[0].Target < 0 {
			continue
		}
		target := bindings[0].Target
		if f.Declarations[target].Kind != "namespace" {
			continue
		}
		r.Bound, r.Target = true, -1
		for _, name := range append(parts[1:], r.Name) {
			if f.Declarations[target].Kind != "namespace" {
				target = -1
				break
			}
			target = member(target, name)
			if target < 0 {
				break
			}
		}
		r.Target = target
	}
	for i := range f.Calls {
		c := &f.Calls[i]
		if c.Receiver == "" {
			continue
		}
		for _, r := range f.References {
			if r.Start < c.Start || r.End > c.End || r.Name != c.Name || r.Receiver != c.Receiver {
				continue
			}
			if r.BindingState() == analysis.Bound && f.Declarations[r.Target].Kind == "function" {
				c.Blocked = false
				for _, span := range closures {
					if span.Start <= c.Start && span.End >= c.End {
						c.Blocked = true
					}
				}
				c.Targets = nil // namespace membership is not a receiver-dispatch hypothesis
			}
			break
		}
	}
}
