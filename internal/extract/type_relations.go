package extract

import (
	gts "github.com/odvcencio/gotreesitter"
	"go/ast"
	"go/token"
)

func extractGoTypeRelations(f *Facts, file *ast.File, fset *token.FileSet) {
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Assign.IsValid() {
			return true
		}
		typ, ok := spec.Type.(*ast.InterfaceType)
		if !ok {
			return true
		}
		owner := -1
		for i, d := range f.Declarations {
			if d.Start == fset.Position(spec.Pos()).Offset {
				owner = i
				break
			}
		}
		if owner < 0 {
			return true
		}
		for _, field := range typ.Methods.List {
			if len(field.Names) > 0 {
				continue
			}
			name, module := goTypeHint(field.Type)
			span := Span{fset.Position(field.Pos()).Offset, fset.Position(field.End()).Offset}
			if name == "" {
				f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "interface type terms are not base-interface bindings", Subject: "relations", Relation: "extends", Span: span})
				continue
			}
			f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: owner, Name: name, Module: module, Kind: "extends", Basis: "interface_embedding", Span: span})
		}
		return true
	})
}

func extractModuleTypeRelations(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	walk(tree.RootNode(), func(n *gts.Node) {
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
			span := Span{int(base.StartByte()), int(base.EndByte())}
			name, module := relationTypeName(base, lang, f.Source)
			// Calls such as mixin(Base) are dynamic base expressions, not a binding
			// to the mixin function as a parent class.
			if base.Type(lang) == "call" || base.Type(lang) == "call_expression" {
				name = ""
			}
			if name == "" {
				f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "base type expression is not a named binding", Subject: "relations", Relation: kind, Span: span})
				return
			}
			allowed := moduleTypeRelationAllowed(f, tree, base, owner, name, module)
			if !allowed {
				f.Issues = append(f.Issues, Issue{Code: "shadowed_type_relation", Message: "base type name has a conflicting lexical binding", Subject: "relations", Relation: kind, Span: span})
				return
			}
			f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: owner, Name: name, Module: module, Kind: kind, Basis: "explicit_" + kind, Span: span})
		}
		if f.Language == "python" {
			if bases := n.ChildByFieldName("superclasses", lang); bases != nil {
				for i := 0; i < bases.NamedChildCount(); i++ {
					base := bases.NamedChild(i)
					if base.Type(lang) != "keyword_argument" {
						add(base, "extends")
					}
				}
			}
			return
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			clause := n.NamedChild(i)
			switch clause.Type(lang) {
			case "class_heritage":
				for j := 0; j < clause.NamedChildCount(); j++ {
					c := clause.NamedChild(j)
					if c.Type(lang) == "extends_clause" || c.Type(lang) == "implements_clause" {
						kind := "extends"
						if c.Type(lang) == "implements_clause" {
							kind = "implements"
						}
						for k := 0; k < c.NamedChildCount(); k++ {
							add(c.NamedChild(k), kind)
						}
					} else {
						add(c, "extends")
					}
				}
			case "extends_type_clause":
				for j := 0; j < clause.NamedChildCount(); j++ {
					add(clause.NamedChild(j), "extends")
				}
			}
		}
	})
}

func relationTypeName(n *gts.Node, lang *gts.Language, source []byte) (string, string) {
	if n == nil {
		return "", ""
	}
	if n.Type(lang) == "generic_type" {
		return relationTypeName(n.ChildByFieldName("name", lang), lang, source)
	}
	if n.Type(lang) == "nested_type_identifier" && n.NamedChildCount() == 2 && n.NamedChild(0).Type(lang) == "identifier" {
		return n.NamedChild(1).Text(source), n.NamedChild(0).Text(source)
	}
	return moduleTypeName(n, lang, source)
}

func moduleTypeRelationAllowed(f *Facts, tree *gts.Tree, use *gts.Node, owner int, name, module string) bool {
	span := Span{int(use.StartByte()), int(use.EndByte())}
	if binding, ok := importBindingForUse(f, name, module, span); ok {
		return !hasBindingConflict(tree, use, Declaration{Kind: "import", Span: binding.Span}, binding.Local)
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
				if hasBindingConflict(tree, use, d, name) {
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
