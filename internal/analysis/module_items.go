package analysis

import (
	"fmt"
	"strings"
)

// ModuleBinding contains source spelling only; resolution lives in edges.
type ModuleBinding struct {
	Specifier, ImportedName, LocalName, ExportedName, Form string
	TypeOnly                                               bool
}

type ExportItem struct {
	Name, Local    string
	Span, NameSpan Span
	TypeOnly       bool
}

type ModuleItem struct {
	Ref            Ref
	Kind, Name     string
	Span, NameSpan Span
	Binding        ModuleBinding
	// Import and BindingIndex address producer records; -1 means a local export.
	Import, BindingIndex, Owner int
}

func ImportItemRef(path string, imp, binding int) Ref {
	return SyntheticRef(fmt.Sprintf("import-item:%q:%d:%d", path, imp, binding))
}
func ExportItemRef(path string, item int) Ref {
	return SyntheticRef(fmt.Sprintf("export-item:%q:%d", path, item))
}

// ModuleItems keeps each imported/exported source item even without an endpoint.
// Statement spans continue to serve lexical lookup; item spans serve navigation.
func ModuleItems(f Facts) []ModuleItem {
	var out []ModuleItem
	for i, imp := range f.Imports {
		specifier := imp.Path
		if imp.From != "" {
			specifier = imp.From
		}
		if imp.Relative > 0 && !strings.HasPrefix(specifier, ".") {
			specifier = strings.Repeat(".", imp.Relative) + specifier
		}
		if len(imp.Bindings) == 0 {
			form, local := "side_effect", imp.Alias
			if f.Language == "go" {
				form = "namespace"
				if local == "_" {
					form, local = "side_effect", ""
				}
				if local == "." {
					form, local = "wildcard", ""
				}
			} else if f.Language == "python" {
				form = "wildcard"
			}
			if imp.Dynamic {
				form = "dynamic"
			}
			out = append(out, ModuleItem{Ref: ImportItemRef(f.Path, i, -1), Kind: "Import", Name: specifier, Span: imp.Span,
				Binding: ModuleBinding{Specifier: specifier, LocalName: local, Form: form}, Import: i, BindingIndex: -1, Owner: EnclosingDeclaration(f, imp.Span)})
		}
		for j, b := range imp.Bindings {
			span := b.ItemSpan
			if span.End <= span.Start {
				span = b.Span
			}
			form := "named"
			if b.Name == "default" {
				form = "default"
			}
			if b.Namespace {
				form = "namespace"
			} else if b.Name == "*" {
				form = "wildcard"
			}
			item := ModuleItem{Ref: ImportItemRef(f.Path, i, j), Kind: "Import", Name: b.Local, Span: span, NameSpan: b.NameSpan,
				Binding: ModuleBinding{Specifier: specifier, ImportedName: b.Name, LocalName: b.Local, Form: form, TypeOnly: b.TypeOnly}, Import: i, BindingIndex: j, Owner: EnclosingDeclaration(f, b.Span)}
			if b.ReExport {
				item.Kind = "Export"
				item.Binding.LocalName = ""
				item.Binding.ExportedName = b.Local
				if form == "wildcard" {
					item.Binding.ExportedName = ""
				}
			}
			out = append(out, item)
		}
	}
	for i, e := range f.ExportItems {
		form := "named"
		if e.Name == "default" {
			form = "default"
		}
		out = append(out, ModuleItem{Ref: ExportItemRef(f.Path, i), Kind: "Export", Name: e.Name, Span: e.Span, NameSpan: e.NameSpan,
			Binding: ModuleBinding{LocalName: e.Local, ExportedName: e.Name, Form: form, TypeOnly: e.TypeOnly}, Import: -1, BindingIndex: -1, Owner: EnclosingDeclaration(f, e.Span)})
	}
	return out
}

func (x *Index) addModuleItems(f Facts) {
	for _, item := range ModuleItems(f) {
		binding := item.Binding
		e := Entity{Ref: item.Ref, Kind: item.Kind, Name: item.Name, Language: f.Language, Location: &SourceLocation{Path: f.Path, Span: item.Span}, Binding: &binding}
		if item.NameSpan.End > item.NameSpan.Start {
			e.NameLocation = &SourceLocation{Path: f.Path, Span: item.NameSpan}
		}
		x.Entities[item.Ref] = e
		x.Add(Edge{Source: item.Ref, Target: SourceRef(f.Path, item.Owner), Kind: "occurs_in", Confidence: Exact, Basis: "source_module_item", Path: f.Path, Span: item.Span})
	}
}
func (x *Index) attachExports(f Facts) {
	for _, item := range ModuleItems(f) {
		if item.Kind != "Export" {
			continue
		}
		owner := x.Roots[f.Path]
		if item.Owner >= 0 {
			owner = DeclarationRef(f.Path, item.Owner)
		}
		if owner == (Ref{}) {
			owner = DocumentRef(f.Path)
		}
		x.Add(Edge{Source: owner, Target: item.Ref, Kind: "exports", Confidence: Exact, Basis: "source_export", Path: f.Path, Span: item.Span})
	}
}

// LocalImport identifies a proven bare import binding, never a member selected
// from it. obj.member refers to the member, not to the obj import itself.
func LocalImport(f Facts, name string, span Span) (Ref, bool) {
	if f.Lexical == nil {
		return Ref{}, false
	}
	visible := f.Lexical.Lookup(name, span)
	if len(visible) != 1 || visible[0].Kind != "import" {
		return Ref{}, false
	}
	for i, imp := range f.Imports {
		for j, b := range imp.Bindings {
			if !b.ReExport && b.Local == name && b.Span == visible[0].Span {
				return ImportItemRef(f.Path, i, j), true
			}
		}
	}
	return Ref{}, false
}
