package syntax

import (
	gts "github.com/odvcencio/gotreesitter"
)

func (x ModuleExtractor) extractModuleTypeRelations(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	Walk(tree.RootNode(), func(n *gts.Node) {
		switch n.Type(lang) {
		case "class_definition", "class_declaration", "abstract_class_declaration", "interface_declaration":
		default:
			return
		}
		owner, size := -1, len(f.Source)+1
		for i, d := range f.Declarations {
			if (d.Kind == "class" || d.Kind == "interface") && d.Start <= int(n.StartByte()) && d.End >= int(n.EndByte()) && d.End-d.Start < size {
				owner, size = i, d.End-d.Start
			}
		}
		if owner < 0 {
			return
		}
		add := func(base *gts.Node, kind string) {
			if base.Type(lang) == "type_arguments" {
				return
			}
			span := Span{Start: int(base.StartByte()), End: int(base.EndByte())}
			name, module := x.relationTypeName(base, lang, f.Source)
			// Calls such as mixin(Base) are dynamic base expressions, not a binding
			// to the mixin function as a parent class.
			if base.Type(lang) == "call" || base.Type(lang) == "call_expression" {
				name = ""
			}
			if name == "" {
				f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "base type expression is not a named binding", Subject: "relations", Relation: kind, Span: span})
				return
			}
			allowed := x.moduleTypeRelationAllowed(f, tree, base, owner, name, module)
			if !allowed {
				f.Issues = append(f.Issues, Issue{Code: "shadowed_type_relation", Message: "base type name has a conflicting lexical binding", Subject: "relations", Relation: kind, Span: span})
				return
			}
			f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: owner, Name: name, Module: module, Kind: kind, Basis: "explicit_" + kind, Span: span})
		}
		x.Dialect.Bases(n, lang, add)
	})
}

func (x ModuleExtractor) relationTypeName(n *gts.Node, lang *gts.Language, source []byte) (string, string) {
	if n == nil {
		return "", ""
	}
	if n.Type(lang) == "generic_type" {
		return x.relationTypeName(n.ChildByFieldName("name", lang), lang, source)
	}
	if n.Type(lang) == "nested_type_identifier" && n.NamedChildCount() == 2 && n.NamedChild(0).Type(lang) == "identifier" {
		return n.NamedChild(1).Text(source), n.NamedChild(0).Text(source)
	}
	return x.moduleTypeName(n, lang, source)
}

func (x ModuleExtractor) moduleTypeRelationAllowed(f *Facts, tree *gts.Tree, use *gts.Node, owner int, name, module string) bool {
	span := Span{Start: int(use.StartByte()), End: int(use.EndByte())}
	if binding, ok := x.importBindingForUse(f, name, module, span); ok {
		return !x.hasBindingConflict(tree, use, Declaration{Kind: "import", Span: binding.Span}, binding.Local)
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
				if x.hasBindingConflict(tree, use, d, name) {
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
