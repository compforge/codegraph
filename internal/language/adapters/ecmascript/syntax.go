package ecmascript

import (
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

func AddModuleImport(f *analysis.Facts, n *gts.Node, lang *gts.Language) {
	raw := n.Text(f.Source)
	span := analysis.Span{Start: int(n.StartByte()), End: int(n.EndByte())}
	if n.Type(lang) != "string" || len(raw) < 2 || strings.Contains(raw, "\\") {
		f.Issues = append(f.Issues, analysis.Issue{Code: "dynamic_import", Message: "import target is not a plain string literal", Subject: "relations", Relation: "imports", Span: span})
		return
	}
	f.Imports = append(f.Imports, analysis.Import{Path: raw[1 : len(raw)-1], Span: span})
}

// importedNames lists the names an import or re-export statement binds from
// its source module, before caller aliases. Namespace and default imports bind
// the whole module, reported as nil.
func ImportedNames(n *gts.Node, lang *gts.Language, source []byte) []string {
	var names []string
	whole := false
	syntax.Walk(n, func(child *gts.Node) {
		switch child.Type(lang) {
		case "import_specifier", "export_specifier":
			if name := child.ChildByFieldName("name", lang); name != nil {
				names = append(names, name.Text(source))
			}
		case "namespace_import":
			whole = true
		case "import_clause":
			for i := 0; i < child.NamedChildCount(); i++ {
				if child.NamedChild(i).Type(lang) == "identifier" {
					whole = true
				}
			}
		}
	})
	if whole {
		return nil
	}
	return names
}
func ImportBindings(n *gts.Node, lang *gts.Language, source []byte) []analysis.ImportBinding {
	var out []analysis.ImportBinding
	span := analysis.Span{Start: int(n.StartByte()), End: int(n.EndByte())}
	reExport := n.Type(lang) == "export_statement"
	syntax.Walk(n, func(child *gts.Node) {
		switch child.Type(lang) {
		case "import_specifier", "export_specifier":
			name := child.ChildByFieldName("name", lang)
			alias := child.ChildByFieldName("alias", lang)
			if name != nil {
				local := name.Text(source)
				if alias != nil {
					local = alias.Text(source)
				}
				out = append(out, analysis.ImportBinding{Name: name.Text(source), Local: local, ReExport: reExport, Span: span})
			}
		case "namespace_export":
			for i := 0; i < child.NamedChildCount(); i++ {
				id := child.NamedChild(i)
				if id.Type(lang) == "identifier" {
					out = append(out, analysis.ImportBinding{Name: "*", Local: id.Text(source), Namespace: true, ReExport: true, Span: span})
				}
			}
		case "namespace_import":
			for i := 0; i < child.NamedChildCount(); i++ {
				if id := child.NamedChild(i); id.Type(lang) == "identifier" {
					out = append(out, analysis.ImportBinding{Local: id.Text(source), Namespace: true, Span: span})
				}
			}
		case "import_clause":
			for i := 0; i < child.NamedChildCount(); i++ {
				if id := child.NamedChild(i); id.Type(lang) == "identifier" {
					out = append(out, analysis.ImportBinding{Name: "default", Local: id.Text(source), Span: span})
				}
			}
		}
	})
	if reExport && len(out) == 0 && strings.Contains(n.Text(source), "*") {
		out = append(out, analysis.ImportBinding{Name: "*", Local: "*", ReExport: true, Span: span})
	}
	return out
}
