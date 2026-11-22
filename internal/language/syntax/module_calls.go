package syntax

import (
	"slices"

	gts "github.com/odvcencio/gotreesitter"
)

func moduleTypeName(n *gts.Node, lang *gts.Language, source []byte, constructors map[Span]gts.CallRef) (string, string) {
	if n == nil {
		return "", ""
	}
	switch n.Type(lang) {
	case "identifier", "type_identifier":
		return n.Text(source), ""
	case "type_annotation", "parenthesized_expression":
		if n.NamedChildCount() > 0 {
			return moduleTypeName(n.NamedChild(0), lang, source, constructors)
		}
	case "new_expression":
		ref := constructors[NodeSpan(n)]
		return ref.Name, ref.Receiver
	case "call", "call_expression":
		return moduleTypeName(n.ChildByFieldName("function", lang), lang, source, constructors)
	case "generic_type":
		return moduleTypeName(n.ChildByFieldName("name", lang), lang, source, constructors)
	case "attribute", "member_expression":
		for _, field := range []string{"attribute", "property"} {
			if member := n.ChildByFieldName(field, lang); member != nil {
				if recv := n.ChildByFieldName("object", lang); recv != nil && recv.Type(lang) == "identifier" {
					return member.Text(source), recv.Text(source)
				}
			}
		}
	}
	return "", ""
}

func (x ModuleExtractor) moduleReceiverHints(f *Facts, tree *gts.Tree, use Span, receiver string) []CallTarget {
	owner := ReferenceOwner(f, use)
	if owner >= 0 && f.Declarations[owner].Parent >= 0 {
		class := f.Declarations[f.Declarations[owner].Parent]
		if class.Kind == "class" {
			self := x.Dialect.Self(f, tree, owner, receiver, x.signatures)
			if self {
				return []CallTarget{{ReceiverType: class.Name, Kind: "method", Basis: "lexical_receiver"}}
			}
		}
	}
	var hints []CallTarget
	for _, binding := range x.lexical.Lookup(receiver, use) {
		hints = append(hints, binding.Hints...)
	}
	return hints
}

func (x ModuleExtractor) extractModuleCalls(f *Facts, tree *gts.Tree, refs []gts.CallRef) {
	lang := tree.Language()
	var closures []Span
	Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(lang) {
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, x.Dialect.Closure(n, lang))
		}
	})
	for _, ref := range refs {
		span := Span{Start: int(ref.StartByte), End: int(ref.EndByte)}
		f.Calls = append(f.Calls, Call{Name: ref.Name, Receiver: ref.Receiver, Span: span, Blocked: true})
		call := &f.Calls[len(f.Calls)-1]
		if slices.ContainsFunc(closures, func(c Span) bool { return c.Start <= span.Start && c.End >= span.End }) {
			continue
		}
		if ref.Kind == "constructor" {
			// A dynamic constructor still contributes a site, without a target.
			if ref.Name != "" && x.moduleConstructorAllowed(f, span, ref.Name, ref.Receiver) {
				call.Targets = append(call.Targets, CallTarget{Name: ref.Name, Module: ref.Receiver, Kind: "constructor", Basis: "constructor_syntax"})
			}
			continue
		}
		node := NodeAtExactRange(tree.RootNode(), ref.StartByte, ref.EndByte)
		if node == nil {
			continue
		}
		if fn := node.ChildByFieldName("function", lang); fn != nil && fn.Type(lang) == "identifier" {
			call.Blocked = false
			var targets []Declaration
			for _, d := range f.Declarations {
				if d.Parent == -1 && d.Kind == "function" && d.Name == ref.Name {
					targets = append(targets, d)
				}
			}
			if len(targets) == 1 {
				call.Blocked = x.hasBindingConflict(span, targets[0].Span, ref.Name)
			}
		}
		if b, ok := x.importBindingForUse(f, ref.Name, ref.Receiver, span); ok && !x.hasBindingConflict(span, b.Span, b.Local) {
			call.Imported, call.Blocked = true, false
		}
		if call.Receiver != "" && !call.Imported {
			hints := x.moduleReceiverHints(f, tree, span, call.Receiver)
			if len(hints) == 0 {
				hints = []CallTarget{{Kind: "method", Basis: "method_name"}}
			}
			for _, hint := range hints {
				hint.Name = call.Name
				call.Targets = append(call.Targets, hint)
			}
		}
		knownClass := call.Imported
		for _, d := range f.Declarations {
			if d.Parent == -1 && d.Kind == "class" && d.Name == call.Name {
				knownClass = true
			}
		}
		if call.Receiver == "" && knownClass && (!call.Blocked || call.Imported) && x.moduleConstructorAllowed(f, span, call.Name, "") {
			// Python class calls share the ordinary call syntax. Resolve a class only
			// when that name denotes one in the supplied local/imported declarations.
			call.Targets = append(call.Targets, CallTarget{Name: call.Name, Kind: "constructor", Basis: "class_call"})
		}
	}
}

func (x ModuleExtractor) moduleConstructorAllowed(f *Facts, span Span, name, module string) bool {
	if binding, ok := x.importBindingForUse(f, name, module, span); ok {
		return !x.hasBindingConflict(span, binding.Span, binding.Local)
	}
	if module == "" {
		for _, d := range f.Declarations {
			if d.Kind == "class" && d.Name == name && d.Parent == -1 {
				return !x.hasBindingConflict(span, d.Span, name)
			}
		}
	}
	return true
}
