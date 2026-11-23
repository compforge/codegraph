package ecmascript

import (
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func addModuleImport(f *Facts, n *gts.Node, lang *gts.Language) *analysis.Import {
	raw := n.Text(f.Source)
	imp := analysis.Import{Path: raw, Span: syntax.NodeSpan(n)}
	if n.Type(lang) == "string" && len(raw) >= 2 && !strings.Contains(raw, "\\") {
		imp.Path = raw[1 : len(raw)-1]
	} else {
		imp.Dynamic = true
		f.Issues = append(f.Issues, analysis.Issue{Code: "dynamic_import", Message: "import target is not a plain string literal", Subject: "relations", Relation: "imports", Span: imp.Span})
	}
	f.Imports = append(f.Imports, imp)
	return &f.Imports[len(f.Imports)-1]
}

// Static imports keep the module specifier separately from the source items it binds.
func addStaticImport(f *Facts, n *gts.Node, lang *gts.Language) {
	source := n.ChildByFieldName("source", lang)
	if source == nil && n.Type(lang) == "import_statement" {
		for i := 0; i < n.NamedChildCount(); i++ {
			if clause := n.NamedChild(i); clause.Type(lang) == "import_require_clause" {
				source = clause.ChildByFieldName("source", lang)
				break
			}
		}
	}
	if source == nil {
		return
	}
	imp := addModuleImport(f, source, lang)
	reExport := n.Type(lang) == "export_statement"
	wholeModule := false
	for _, item := range grammars.ImportItems(n, lang) {
		b := analysis.ImportBinding{
			Name:     string(f.Source[item.NameRange.StartByte:item.NameRange.EndByte]),
			Local:    string(f.Source[item.BindingRange.StartByte:item.BindingRange.EndByte]),
			ReExport: reExport, Span: syntax.NodeSpan(n), TypeOnly: item.TypeOnly,
			ItemSpan: syntax.ByteSpan(item.Range), NameSpan: syntax.ByteSpan(item.BindingRange),
		}
		switch item.Kind {
		case "named":
			imp.Names = append(imp.Names, b.Name)
		case "default":
			b.Name = "default"
			wholeModule = true
		case "namespace", "module":
			b.Namespace = true
			if !reExport {
				b.Name = ""
			}
			wholeModule = true
		case "wildcard":
			b.Local = "*"
		}
		imp.Bindings = append(imp.Bindings, b)
	}
	if wholeModule {
		imp.Names = nil
	}
}

func bindingNameSpan(name, alias *gts.Node) analysis.Span {
	if alias != nil {
		return syntax.NodeSpan(alias)
	}
	return syntax.NodeSpan(name)
}
