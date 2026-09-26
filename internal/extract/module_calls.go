package extract

import (
	gts "github.com/odvcencio/gotreesitter"
)

func moduleTypeName(n *gts.Node, lang *gts.Language, source []byte) (string, string) {
	if n == nil {
		return "", ""
	}
	switch n.Type(lang) {
	case "identifier", "type_identifier":
		return n.Text(source), ""
	case "type_annotation", "parenthesized_expression":
		if n.NamedChildCount() > 0 {
			return moduleTypeName(n.NamedChild(0), lang, source)
		}
	case "new_expression":
		return moduleTypeName(n.ChildByFieldName("constructor", lang), lang, source)
	case "call", "call_expression":
		return moduleTypeName(n.ChildByFieldName("function", lang), lang, source)
	case "generic_type":
		return moduleTypeName(n.ChildByFieldName("name", lang), lang, source)
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

func moduleReceiverHints(f *Facts, tree *gts.Tree, use *gts.Node, receiver string) []CallTarget {
	lang := tree.Language()
	owner := referenceOwner(f, Span{int(use.StartByte()), int(use.EndByte())})
	if owner >= 0 && f.Declarations[owner].Parent >= 0 {
		class := f.Declarations[f.Declarations[owner].Parent]
		if class.Kind == "class" {
			self := receiver == "this"
			if f.Language == "python" {
				walk(tree.RootNode(), func(n *gts.Node) {
					if int(n.StartByte()) != f.Declarations[owner].Start {
						return
					}
					if params := n.ChildByFieldName("parameters", lang); params != nil && params.NamedChildCount() > 0 {
						self = params.NamedChild(0).Text(f.Source) == receiver
					}
				})
			}
			if self {
				return []CallTarget{{ReceiverType: class.Name, Kind: "method", Basis: "lexical_receiver"}}
			}
		}
	}
	var hints []CallTarget
	walk(tree.RootNode(), func(n *gts.Node) {
		typ := n.Type(lang)
		var binding *gts.Node
		switch typ {
		case "variable_declarator", "required_parameter", "optional_parameter":
			binding = n.ChildByFieldName("name", lang)
			if binding == nil {
				binding = n.ChildByFieldName("pattern", lang)
			}
		case "assignment", "typed_parameter":
			binding = n.ChildByFieldName("left", lang)
			if binding == nil && n.NamedChildCount() > 0 {
				binding = n.NamedChild(0)
			}
		}
		if binding == nil || binding.Text(f.Source) != receiver {
			return
		}
		scope := referenceOwner(f, Span{int(n.StartByte()), int(n.EndByte())})
		for scope >= 0 && (f.Declarations[scope].Kind == "variable" || f.Declarations[scope].Kind == "constant") {
			scope = f.Declarations[scope].Parent
		}
		if scope >= 0 && (f.Declarations[scope].Start > int(use.StartByte()) || f.Declarations[scope].End < int(use.EndByte())) {
			return
		}
		for _, field := range []string{"type", "value", "right"} {
			value := n.ChildByFieldName(field, lang)
			if value == nil {
				continue
			}
			name, module := moduleTypeName(value, lang, f.Source)
			if name != "" {
				hints = append(hints, CallTarget{ReceiverType: name, Module: module, Kind: "method", Basis: "receiver_binding"})
			}
		}
	})
	return hints
}

func enrichModuleCallTargets(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	nodes := map[Span]*gts.Node{}
	var closures []Span
	walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{int(n.StartByte()), int(n.EndByte())}
		switch n.Type(lang) {
		case "call", "call_expression", "new_expression":
			nodes[span] = n
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, span)
		}
	})
	// Upstream call facts omit new-expressions in some grammars. Capture the
	// source syntax here so constructor evidence does not depend on that omission.
	walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "new_expression" {
			return
		}
		span := Span{int(n.StartByte()), int(n.EndByte())}
		for _, call := range f.Calls {
			if call.Span == span {
				return
			}
		}
		name, module := moduleTypeName(n.ChildByFieldName("constructor", lang), lang, f.Source)
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
			name, module := moduleTypeName(node.ChildByFieldName("constructor", lang), lang, f.Source)
			if name != "" && moduleConstructorAllowed(f, tree, node, name, module) {
				call.Targets = append(call.Targets, CallTarget{Name: name, Module: module, Kind: "constructor", Basis: "constructor_syntax"})
			}
			continue
		}
		if call.Receiver != "" && !call.Imported {
			hints := moduleReceiverHints(f, tree, node, call.Receiver)
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
		if call.Receiver == "" && knownClass && (!call.Blocked || call.Imported) && moduleConstructorAllowed(f, tree, node, call.Name, "") {
			// Python class calls share the ordinary call syntax. Resolve a class only
			// when that name denotes one in the supplied local/imported declarations.
			call.Targets = append(call.Targets, CallTarget{Name: call.Name, Kind: "constructor", Basis: "class_call"})
		}
	}
}

func moduleConstructorAllowed(f *Facts, tree *gts.Tree, n *gts.Node, name, module string) bool {
	span := Span{int(n.StartByte()), int(n.EndByte())}
	if binding, ok := importBindingForUse(f, name, module, span); ok {
		return !hasBindingConflict(tree, n, Declaration{Kind: "import", Span: binding.Span}, binding.Local)
	}
	if module == "" {
		for _, d := range f.Declarations {
			if d.Kind == "class" && d.Name == name && d.Parent == -1 {
				return !hasBindingConflict(tree, n, d, name)
			}
		}
	}
	return true
}
