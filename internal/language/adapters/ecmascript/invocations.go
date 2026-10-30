package ecmascript

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// extractInvocations preserves syntax and binding facts without library-specific effects.
// +spec=Invocation facts do not depend on configured resource rules and are safe to cache across consumers.
func extractInvocations(ctx context.Context, f *Facts, tree *gts.Tree, extracted gts.FactSet) error {
	arguments := syntax.ArgumentsByCall(extracted.CallArguments)
	// All argument spellings share one immutable string, including nested calls.
	source := string(f.Source)
	lang := tree.Language()
	var cancelled error
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		if cancelled != nil {
			return
		}
		if cancelled = ctx.Err(); cancelled != nil {
			return
		}
		typ := n.Type(lang)
		if typ != "call_expression" && typ != "new_expression" {
			return
		}
		field := "function"
		if typ == "new_expression" {
			field = "constructor"
		}
		callee := n.ChildByFieldName(field, lang)
		if callee == nil {
			return
		}
		call := analysis.Invocation{Span: syntax.NodeSpan(n), Constructor: typ == "new_expression"}
		call.Owner = analysis.EnclosingDeclaration(*f, call.Span)
		switch callee.Type(lang) {
		case "identifier":
			call.Name = callee.Text(f.Source)
		case "member_expression":
			object, property := callee.ChildByFieldName("object", lang), callee.ChildByFieldName("property", lang)
			if object == nil || property == nil || object.Type(lang) != "identifier" || property.Type(lang) != "property_identifier" {
				return
			}
			call.Name, call.Receiver = property.Text(f.Source), object.Text(f.Source)
		default:
			return
		}
		call.Target = invocationTarget(*f, call.Name, call.Receiver, call.Span)
		for _, arg := range arguments[call.Span] {
			span := analysis.Span{Start: int(arg.StartByte), End: int(arg.EndByte)}
			value := analysis.Argument{Spread: arg.Spread, Text: source[span.Start:span.End], Span: span}
			if !arg.Spread {
				value.ModuleURL = value.Text == "import.meta.url"
				// Preserve unsupported forms as text; do not pretend to evaluate JS escapes.
				if arg.NodeType == "string" && len(value.Text) >= 2 && !strings.Contains(value.Text, "\\") {
					literal := value.Text[1 : len(value.Text)-1]
					value.Literal = &literal
				}
			}
			call.Arguments = append(call.Arguments, value)
		}
		f.Invocations = append(f.Invocations, call)
	})
	return cancelled
}

// Imported identities follow lexical bindings. Unbound bare names are explicit
// global candidates; only caller-provided rules can assign effects to them.
func invocationTarget(f Facts, name, receiver string, use analysis.Span) *analysis.Callable {
	local := name
	if receiver != "" {
		local = receiver
	}
	ref, ok := analysis.LocalImport(f, local, use)
	if !ok {
		if receiver == "" && len(f.Lexical.Lookup(name, use)) == 0 {
			return &analysis.Callable{Name: name}
		}
		return nil
	}
	for _, item := range analysis.ModuleItems(f) {
		b := item.Binding
		if item.Ref != ref || b.TypeOnly {
			continue
		}
		if receiver == "" && (b.Form == "named" || b.Form == "default") {
			return &analysis.Callable{Module: b.Specifier, Name: b.ImportedName}
		}
		if receiver != "" && (b.Form == "namespace" || b.Form == "default") {
			return &analysis.Callable{Module: b.Specifier, Name: name}
		}
	}
	return nil
}
