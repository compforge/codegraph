package syntax

import (
	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) moduleTypeName(n *gts.Node, lang *gts.Language, source []byte) (string, string) {
	if n == nil {
		return "", ""
	}
	switch n.Type(lang) {
	case "identifier", "type_identifier":
		return n.Text(source), ""
	case "type_annotation", "parenthesized_expression":
		if n.NamedChildCount() > 0 {
			return x.moduleTypeName(n.NamedChild(0), lang, source)
		}
	case "new_expression":
		return x.moduleTypeName(n.ChildByFieldName("constructor", lang), lang, source)
	case "call", "call_expression":
		return x.moduleTypeName(n.ChildByFieldName("function", lang), lang, source)
	case "generic_type":
		return x.moduleTypeName(n.ChildByFieldName("name", lang), lang, source)
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

func (x ModuleExtractor) moduleReceiverHints(f *Facts, tree *gts.Tree, use *gts.Node, receiver string) []CallTarget {
	owner := ReferenceOwner(f, Span{Start: int(use.StartByte()), End: int(use.EndByte())})
	if owner >= 0 && f.Declarations[owner].Parent >= 0 {
		class := f.Declarations[f.Declarations[owner].Parent]
		if class.Kind == "class" {
			self := x.Dialect.Self(f, tree, owner, receiver)
			if self {
				return []CallTarget{{ReceiverType: class.Name, Kind: "method", Basis: "lexical_receiver"}}
			}
		}
	}
	var hints []CallTarget
	for _, binding := range x.lexical.Lookup(receiver, NodeSpan(use)) {
		hints = append(hints, binding.Hints...)
	}
	return hints
}

func (x ModuleExtractor) enrichModuleCallTargets(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	nodes := map[Span]*gts.Node{}
	var closures []Span
	Walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
		switch n.Type(lang) {
		case "call", "call_expression", "new_expression":
			nodes[span] = n
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, x.moduleClosureSpan(n, lang))
		}
	})
	// Upstream call facts omit new-expressions in some grammars. Capture the
	// source syntax here so constructor evidence does not depend on that omission.
	Walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "new_expression" {
			return
		}
		span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
		for _, call := range f.Calls {
			if call.Span == span {
				return
			}
		}
		name, module := x.moduleTypeName(n.ChildByFieldName("constructor", lang), lang, f.Source)
		f.Calls = append(f.Calls, Call{Name: name, Receiver: module, Span: span, Blocked: true})
	})
	for i := range f.Calls {
		call := &f.Calls[i]
		hidden := false
		for _, span := range closures {
			if span.Start <= call.Start && span.End >= call.End {
				hidden = true
			}
		}
		if hidden {
			continue
		}
		node := nodes[call.Span]
		if node == nil {
			continue
		}
		if node.Type(lang) == "new_expression" {
			name, module := x.moduleTypeName(node.ChildByFieldName("constructor", lang), lang, f.Source)
			if name != "" && x.moduleConstructorAllowed(f, tree, node, name, module) {
				call.Targets = append(call.Targets, CallTarget{Name: name, Module: module, Kind: "constructor", Basis: "constructor_syntax"})
			}
			continue
		}
		if call.Receiver != "" && !call.Imported {
			hints := x.moduleReceiverHints(f, tree, node, call.Receiver)
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
		if call.Receiver == "" && knownClass && (!call.Blocked || call.Imported) && x.moduleConstructorAllowed(f, tree, node, call.Name, "") {
			// Python class calls share the ordinary call syntax. Resolve a class only
			// when that name denotes one in the supplied local/imported declarations.
			call.Targets = append(call.Targets, CallTarget{Name: call.Name, Kind: "constructor", Basis: "class_call"})
		}
	}
}

func (x ModuleExtractor) moduleConstructorAllowed(f *Facts, tree *gts.Tree, n *gts.Node, name, module string) bool {
	span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
	if binding, ok := x.importBindingForUse(f, name, module, span); ok {
		return !x.hasBindingConflict(tree, n, Declaration{Kind: "import", Span: binding.Span}, binding.Local)
	}
	if module == "" {
		for _, d := range f.Declarations {
			if d.Kind == "class" && d.Name == name && d.Parent == -1 {
				return !x.hasBindingConflict(tree, n, d, name)
			}
		}
	}
	return true
}

// Python evaluates lambda defaults when creating the function. Only its body
// has deferred execution; keep the existing conservative span for JS/TS.
func (x ModuleExtractor) moduleClosureSpan(n *gts.Node, lang *gts.Language) Span {
	return x.Dialect.Closure(n, lang)
}
