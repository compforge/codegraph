package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// Queries only report candidates they matched. Recognize declaration syntax
// independently so an unhandled shape remains visible in this build's report.
func recordDeclarationCoverage(f *Facts, tree *gts.Tree) {
	var omitted []analysis.Span
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(tree.Language()) {
		case "internal_module", "module", "function_signature", "abstract_class_declaration",
			"abstract_method_signature", "method_signature", "property_signature", "public_field_definition",
			"field_definition", "variable_declarator", "index_signature", "call_signature", "construct_signature":
		default:
			return
		}
		span := syntax.NodeSpan(n)
		for _, d := range f.Declarations {
			if d.Span == span {
				return
			}
		}
		f.Issues = append(f.Issues, analysis.Issue{Code: "unsupported_declaration", Subject: "declarations", Message: n.Type(tree.Language()), Span: span})
		if n.Type(tree.Language()) == "internal_module" || n.Type(tree.Language()) == "module" || n.Type(tree.Language()) == "abstract_class_declaration" {
			omitted = append(omitted, span)
		}
	})
	// Preserve the owner's gap, rather than promoting descendants to module members.
	remap := map[int]int{}
	kept := make([]analysis.Declaration, 0, len(f.Declarations))
	for i, d := range f.Declarations {
		skip := false
		for _, span := range omitted {
			if span.Start <= d.Start && span.End >= d.End {
				skip = true
				break
			}
		}
		if !skip {
			remap[i] = len(kept)
			kept = append(kept, d)
		}
	}
	for i := range kept {
		if kept[i].Parent >= 0 {
			kept[i].Parent = remap[kept[i].Parent]
		}
	}
	f.Declarations = kept
	seen := map[string]bool{}
	for _, d := range f.Declarations {
		if d.Kind != "namespace" {
			continue
		}
		if seen[d.QualifiedName] {
			f.Issues = append(f.Issues, analysis.Issue{Code: "unsupported_namespace_merge", Subject: "relations", Message: d.QualifiedName, Span: d.Span})
		}
		seen[d.QualifiedName] = true
	}
}
