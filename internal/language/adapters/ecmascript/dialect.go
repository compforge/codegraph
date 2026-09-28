package ecmascript

import (
	"strings"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

type Dialect struct{}

func (Dialect) Enrich(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	if f.Exports == nil {
		f.Exports = map[string]string{}
	}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "export_statement" || n.ChildByFieldName("source", lang) != nil {
			return
		}
		for _, field := range []string{"declaration", "value"} {
			if d := n.ChildByFieldName(field, lang); d != nil {
				for _, decl := range f.Declarations {
					if decl.Parent == -1 && int(d.StartByte()) <= decl.Start && int(d.EndByte()) >= decl.End {
						name := decl.Name
						if strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
							name = "default"
						}
						f.Exports[name] = decl.Name
					}
				}
				if d.Type(lang) == "identifier" && strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
					f.Exports["default"] = d.Text(f.Source)
				}
			}
		}
		syntax.Walk(n, func(spec *gts.Node) {
			if spec.Type(lang) != "export_specifier" {
				return
			}
			name := spec.ChildByFieldName("name", lang)
			alias := spec.ChildByFieldName("alias", lang)
			if name != nil {
				public := name.Text(f.Source)
				if alias != nil {
					public = alias.Text(f.Source)
				}
				f.Exports[public] = name.Text(f.Source)
			}
		})
	})

	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		typ := n.Type(lang)
		if typ == "import_statement" || typ == "export_statement" {
			if src := n.ChildByFieldName("source", lang); src != nil {
				start := len(f.Imports)
				AddModuleImport(f, src, lang)
				if len(f.Imports) > start {
					f.Imports[start].Names = ImportedNames(n, lang, f.Source)
					f.Imports[start].Bindings = ImportBindings(n, lang, f.Source)
				}
			} else if typ == "export_statement" {
				syntax.Walk(n, func(child *gts.Node) {
					if child.Type(lang) != "export_specifier" {
						return
					}
					local := child.ChildByFieldName("name", lang)
					alias := child.ChildByFieldName("alias", lang)
					if local != nil && alias != nil {
						if f.Exports == nil {
							f.Exports = map[string]string{}
						}
						f.Exports[alias.Text(f.Source)] = local.Text(f.Source)
					}
				})
			}
		}
		if typ == "call_expression" {
			fn := n.ChildByFieldName("function", lang)
			if fn == nil {
				return
			}
			switch fn.Text(f.Source) {
			case "import", "require", "require.resolve":
				args := n.ChildByFieldName("arguments", lang)
				if args != nil && args.NamedChildCount() == 1 {
					AddModuleImport(f, args.NamedChild(0), lang)
				}
			}
		}
	})
}

func (Dialect) Parameters(n *gts.Node, lang *gts.Language) ([]*gts.Node, []syntax.ParameterExpression, bool) {
	return nil, nil, false
}
func (Dialect) SignatureScope(n, use *gts.Node, lang *gts.Language) bool { return false }
func (Dialect) Self(f *Facts, tree *gts.Tree, owner int, receiver string) bool {
	return receiver == "this"
}
func (Dialect) Bases(n *gts.Node, lang *gts.Language, add func(*gts.Node, string)) {

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
}
func (Dialect) Closure(n *gts.Node, lang *gts.Language) Span {
	return Span{Start: int(n.StartByte()), End: int(n.EndByte())}
}
