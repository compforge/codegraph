package ecmascript

import (
	"github.com/compforge/codegraph/internal/analysis"
	"strings"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

type Dialect struct{}

func (Dialect) Enrich(f *Facts, tree *gts.Tree) {
	recordDeclarationCoverage(f, tree)
	attachDocumentation(f, tree)
	lang := tree.Language()
	if f.Exports == nil {
		f.Exports = map[string]string{}
	}
	syntax.Walk(tree.RootNode(), func(n *gts.Node) {
		if n.Type(lang) != "export_statement" || n.ChildByFieldName("source", lang) != nil {
			return
		}
		before := len(f.ExportItems)
		owner := analysis.EnclosingDeclaration(*f, syntax.NodeSpan(n))
		for _, field := range []string{"declaration", "value"} {
			if d := n.ChildByFieldName(field, lang); d != nil {
				for _, decl := range f.Declarations {
					if decl.Parent == owner && int(d.StartByte()) <= decl.Start && int(d.EndByte()) >= decl.End {
						name := decl.Name
						if strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
							name = "default"
						}
						recordExport(f, name, decl.Name, syntax.NodeSpan(n), decl.NameSpan, false)
					}
				}
				if d.Type(lang) == "identifier" && strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
					recordExport(f, "default", d.Text(f.Source), syntax.NodeSpan(n), syntax.NodeSpan(d), false)
				}
			}
		}
		if len(f.ExportItems) == before && strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export default") {
			recordExport(f, "default", "", syntax.NodeSpan(n), analysis.Span{}, false)
			f.Issues = append(f.Issues, analysis.Issue{Code: "unresolved_export", Message: "default expression has no retained declaration", Subject: "relations", Relation: "exports", Span: syntax.NodeSpan(n)})
		}
		for j := 0; j < n.NamedChildCount(); j++ {
			clause := n.NamedChild(j)
			if clause.Type(lang) != "export_clause" {
				continue
			}
			syntax.Walk(clause, func(spec *gts.Node) {
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
					recordExport(f, public, name.Text(f.Source), syntax.NodeSpan(spec), bindingNameSpan(name, alias), strings.HasPrefix(strings.TrimSpace(n.Text(f.Source)), "export type ") || strings.HasPrefix(spec.Text(f.Source), "type "))
				}
			})
		}
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

// Export records own source positions; the map is only a resolver lookup index.
func recordExport(f *Facts, name, local string, span, nameSpan analysis.Span, typeOnly bool) {
	if analysis.EnclosingDeclaration(*f, span) < 0 {
		f.Exports[name] = local
	}
	for _, e := range f.ExportItems {
		if e.Name == name && e.Span == span {
			return
		}
	}
	f.ExportItems = append(f.ExportItems, analysis.ExportItem{Name: name, Local: local, Span: span, NameSpan: nameSpan, TypeOnly: typeOnly})
}
