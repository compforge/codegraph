package syntax

import (
	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
)

func NodeSpan(n *gts.Node) Span {
	if n == nil {
		return Span{}
	}
	return Span{Start: int(n.StartByte()), End: int(n.EndByte())}
}

// AddLexicalBinding records only identifier leaves selected by the adapter.
func AddLexicalBinding(l *analysis.Lexicon, f *Facts, tree *gts.Tree, scope int, name, owner *gts.Node, kind string) {
	if name == nil {
		return
	}
	span, at := NodeSpan(owner), NodeSpan(name)
	b := analysis.Binding{Name: name.Text(f.Source), Kind: kind, Scope: scope, Target: -1, Span: span, NameSite: at}
	for i, d := range f.Declarations {
		if d.Name == b.Name && (d.Span == span || d.Start == at.Start) {
			b.Target = i
			break
		}
	}
	for _, field := range []string{"type", "value", "right"} {
		value := owner.ChildByFieldName(field, tree.Language())
		if value == nil {
			continue
		}
		name, module := (ModuleExtractor{}).moduleTypeName(value, tree.Language(), f.Source)
		if name != "" {
			b.Hints = append(b.Hints, CallTarget{ReceiverType: name, Module: module, Kind: "method", Basis: "receiver_binding"})
		}
	}
	l.Add(b)
}
func AddLexicalImports(l *analysis.Lexicon, f *Facts) {
	for _, imp := range f.Imports {
		for _, b := range imp.Bindings {
			if !b.ReExport {
				l.Add(analysis.Binding{Name: b.Local, Kind: "import", Scope: l.ScopeAt(b.Span), Target: -1, Span: b.Span})
			}
		}
	}
}
