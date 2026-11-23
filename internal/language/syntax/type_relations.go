package syntax

import (
	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) extractModuleTypeRelations(f *Facts, tree *gts.Tree, refs []gts.HeritageRef) {
	if len(refs) == 0 {
		return
	}
	owners := make(map[Span]int)
	for i, d := range f.Declarations {
		if d.Kind == "class" || d.Kind == "interface" {
			owners[d.Span] = i
		}
	}
	lang := tree.Language()
	for _, ref := range refs {
		owner, ok := owners[Span{Start: int(ref.StartByte), End: int(ref.EndByte)}]
		if !ok {
			continue
		}
		kind := ref.Kind
		if kind == "base" {
			kind = "extends"
		}
		span := Span{Start: int(ref.ParentStartByte), End: int(ref.ParentEndByte)}
		base := tree.RootNode().NamedDescendantForByteRange(ref.ParentStartByte, ref.ParentEndByte)
		// TypeScript keeps the value and its type arguments as siblings.
		if base != nil && base.Type(lang) == "extends_clause" {
			base = base.ChildByFieldName("value", lang)
		}
		var name, module string
		if base != nil && base.StartByte() == ref.ParentStartByte && base.Type(lang) == ref.ParentNodeType {
			name, module = x.relationTypeName(base, lang, f.Source)
		}
		blocked := name == ""
		if blocked {
			f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "base type expression is not a named binding", Subject: "relations", Relation: kind, Span: span})
			name = ref.Parent
		}
		allowed := !blocked && x.moduleTypeRelationAllowed(f, span, owner, name, module)
		if !allowed && !blocked {
			f.Issues = append(f.Issues, Issue{Code: "shadowed_type_relation", Message: "base type name has a conflicting lexical binding", Subject: "relations", Relation: kind, Span: span})
		}
		f.TypeRelations = append(f.TypeRelations, TypeRelation{Blocked: !allowed, Owner: owner, Name: name, Module: module, Kind: kind, Basis: "explicit_" + kind, Span: span})
	}
}

func (x ModuleExtractor) relationTypeName(n *gts.Node, lang *gts.Language, source []byte) (string, string) {
	if n == nil || n.Type(lang) == "call" || n.Type(lang) == "call_expression" {
		return "", ""
	}
	if n.Type(lang) == "generic_type" {
		return x.relationTypeName(n.ChildByFieldName("name", lang), lang, source)
	}
	if n.Type(lang) == "nested_type_identifier" && n.NamedChildCount() == 2 && n.NamedChild(0).Type(lang) == "identifier" {
		return n.NamedChild(1).Text(source), n.NamedChild(0).Text(source)
	}
	return moduleTypeName(n, lang, source, nil)
}

func (x ModuleExtractor) moduleTypeRelationAllowed(f *Facts, span Span, owner int, name, module string) bool {
	if binding, ok := x.importBindingForUse(f, name, module, span); ok {
		return !x.hasBindingConflict(span, binding.Span, binding.Local)
	}
	if module != "" {
		return true
	}
	scope := f.Declarations[owner].Parent
	for {
		found := false
		for _, d := range f.Declarations {
			if d.Parent == scope && d.Name == name && (d.Kind == "class" || d.Kind == "interface") {
				found = true
				if x.hasBindingConflict(span, d.Span, name) {
					return false
				}
			}
		}
		if found || scope == -1 {
			return true
		}
		scope = f.Declarations[scope].Parent
	}
}
