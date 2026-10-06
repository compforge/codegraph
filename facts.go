package codegraph

import (
	"fmt"

	"github.com/compforge/codegraph/internal/analysis"
)

// Facts owns the complete immutable extraction artifact for one document
// version for graph production. Graph consumers read Node and Relation values;
// Facts are inputs for dependency exploration, resolution and Builder.Add.
// Copying it shares immutable material, including language-specific
// evidence needed for binding. Exported fields are detached inspection views:
// changing them does not change what Builder.Add consumes. Use View for a fresh
// projection. Only extraction APIs can produce valid Facts.
type Facts struct {
	raw                     *analysis.Facts
	Path, Package, Language string
	// Gitlink retains the pinned commit for an opaque gitlink document.
	Gitlink       string
	DocumentKind  DocumentKind
	Manifest      *ManifestMetadata
	Declarations  []FactDeclaration
	Imports       []FactImport
	Calls         []FactCall
	References    []FactReference
	TypeRelations []FactTypeRelation
	Issues        []Diagnostic
	// Exports maps explicit public names to local declarations or imported bindings.
	// Cross-module re-exports are recorded on Imports.Bindings.
	Exports     map[string]string
	ExportItems []FactExport
	// Statements preserve execution order and scope with a bounded expression
	// vocabulary, for producer-side dependency resolution; the graph model never
	// depends on statement-level facts. Captured for Python sources today,
	// empty for languages where statement capture is not implemented.
	Statements []Statement
}

// Statement preserves execution order and scope without evaluating code.
// Kind values are grammar node types of the capturing language.
type Statement struct {
	Kind, Name    string
	Line          int // One-based source line.
	Imports       []FactImport
	Target, Value Expression
	Prelude       []Expression
	Body, Else    []Statement
}

type Expression struct {
	Kind, Text string
	Children   []Expression
}

type FactDeclaration struct {
	Name, QualifiedName string
	Kind                NodeKind
	Location            Location
	NameLocation        *Location
	Signature           string
	SignatureLocation   *Location
	Markers             []Marker
	Documentation       []Documentation
}

type FactImport struct {
	// Dynamic means Path is unevaluated source text, not a module specifier.
	Dynamic           bool
	Alias, Path, From string
	Relative          int
	// Binding is the name the import introduces in this lexical scope.
	Binding string
	// Names are the imported names before caller aliases; empty means the
	// whole module is imported.
	Names    []string
	Bindings []FactImportBinding
	Location Location
}

// FactImportBinding preserves a source name, its local alias and its statement scope.
type FactImportBinding struct {
	Name, Local                string
	Namespace, ReExport        bool
	TypeOnly                   bool
	ItemLocation, NameLocation *Location
	Location                   Location
}

// FactReference records one identifier use. Owner indexes Declarations, or is
// -1 for file scope. A missing target does not discard the lexical fact.
type FactReference struct {
	Kind ReferenceKind
	// Decorated is set for DecoratorReference and indexes Declarations.
	Decorated      int
	Name, Receiver string
	Location       Location
	Owner          int
}

// FactCallTarget records a syntax-based callable candidate before graph binding.
// Kind describes the callable form; Constructor candidates bind to Class nodes.
// Module is a source import qualifier, not a fetched dependency identity.
type FactCallTarget struct {
	Name, ReceiverType, Module string
	Kind                       NodeKind
	Basis                      string
}

// FactTypeRelation records explicit inheritance/interface syntax. Owner indexes
// Declarations; Name and Module retain the unbound source spelling.
type FactTypeRelation struct {
	Blocked      bool
	Owner        int
	Name, Module string
	Kind         RelationKind
	Basis        string
	Location     Location
}

// FactExport preserves an explicit local export independently of its target.
type FactExport struct {
	Name, Local  string
	Location     Location
	NameLocation *Location
	TypeOnly     bool
}

type FactCall struct {
	Name, Receiver string
	Location       Location
	// Blocked excludes the static-function path; Targets may still carry dispatch candidates.
	Blocked, Builtin bool
	Targets          []FactCallTarget
}

func projectFacts(f analysis.Facts) (Facts, error) {
	out := Facts{raw: &f, Path: f.Path, Package: f.Package, Language: f.Language, Gitlink: f.Gitlink, DocumentKind: materialKind(f.Path, f.Gitlink), Manifest: projectManifest(f)}
	for _, d := range f.Declarations {
		kind, err := declarationKind(d.Kind)
		if err != nil {
			return Facts{}, fmt.Errorf("%s: %w", f.Path, err)
		}
		decl := FactDeclaration{Name: d.Name, QualifiedName: d.QualifiedName, Kind: kind, Location: location(f, d.Span)}
		if d.SignatureSpan.End > d.SignatureSpan.Start {
			decl.Signature = string(f.Source[d.SignatureSpan.Start:d.SignatureSpan.End])
			decl.SignatureLocation = locationPtr(f, d.SignatureSpan)
		}
		if d.NameSpan.End > d.NameSpan.Start {
			decl.NameLocation = locationPtr(f, d.NameSpan)
		}
		for _, m := range d.Comments {
			decl.Markers = append(decl.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
		}
		decl.Documentation = projectDocumentation(f, d.Documentation)
		out.Declarations = append(out.Declarations, decl)
	}
	imports := make([]FactImport, 0, len(f.Imports))
	byStart := map[int][]int{}
	for j, i := range f.Imports {
		byStart[i.Span.Start] = append(byStart[i.Span.Start], j)
		bindings := make([]FactImportBinding, 0, len(i.Bindings))
		for _, b := range i.Bindings {
			binding := FactImportBinding{Name: b.Name, Local: b.Local, Namespace: b.Namespace, ReExport: b.ReExport, TypeOnly: b.TypeOnly, Location: location(f, b.Span)}
			if b.ItemSpan.End > b.ItemSpan.Start {
				binding.ItemLocation = locationPtr(f, b.ItemSpan)
			}
			if b.NameSpan.End > b.NameSpan.Start {
				binding.NameLocation = locationPtr(f, b.NameSpan)
			}
			bindings = append(bindings, binding)
		}
		imports = append(imports, FactImport{Dynamic: i.Dynamic, Bindings: bindings, Alias: i.Alias, Path: i.Path, From: i.From, Relative: i.Relative, Binding: i.Binding, Names: append([]string(nil), i.Names...), Location: location(f, i.Span)})
	}
	out.Imports = imports
	for _, s := range f.Statements {
		out.Statements = append(out.Statements, projectStatement(s, imports, byStart))
	}
	for public, local := range f.Exports {
		if out.Exports == nil {
			out.Exports = map[string]string{}
		}
		out.Exports[public] = local
	}
	for _, e := range f.ExportItems {
		item := FactExport{Name: e.Name, Local: e.Local, Location: location(f, e.Span), TypeOnly: e.TypeOnly}
		if e.NameSpan.End > e.NameSpan.Start {
			item.NameLocation = locationPtr(f, e.NameSpan)
		}
		out.ExportItems = append(out.ExportItems, item)
	}
	for _, c := range f.Calls {
		targets := make([]FactCallTarget, 0, len(c.Targets))
		for _, target := range c.Targets {
			kind := Function
			if target.Kind == "method" {
				kind = Method
			}
			if target.Kind == "constructor" {
				kind = Constructor
			}
			targets = append(targets, FactCallTarget{Name: target.Name, ReceiverType: target.ReceiverType, Module: target.Module, Kind: kind, Basis: target.Basis})
		}
		out.Calls = append(out.Calls, FactCall{Targets: targets, Name: c.Name, Receiver: c.Receiver, Location: location(f, c.Span), Blocked: c.Blocked, Builtin: c.Builtin})
	}
	for _, r := range f.References {
		kind, decorated := SymbolReference, -1
		if r.Kind != "" {
			kind = ReferenceKind(r.Kind)
		}
		if kind == DecoratorReference {
			decorated = r.Decorated
		}
		out.References = append(out.References, FactReference{Kind: kind, Decorated: decorated, Name: r.Name, Receiver: r.Receiver, Location: location(f, r.Span), Owner: r.Owner})
	}
	for _, r := range f.TypeRelations {
		out.TypeRelations = append(out.TypeRelations, FactTypeRelation{Blocked: r.Blocked, Owner: r.Owner, Name: r.Name, Module: r.Module, Kind: RelationKind(r.Kind), Basis: r.Basis, Location: location(f, r.Span)})
	}
	for _, issue := range f.Issues {
		out.Issues = append(out.Issues, extractionDiagnostic(f, issue))
	}
	return out, nil
}

func projectDocumentation(f analysis.Facts, docs []analysis.Documentation) []Documentation {
	var out []Documentation
	for _, doc := range docs {
		out = append(out, Documentation{Text: doc.Text, Location: location(f, doc.Span)})
	}
	return out
}

func projectExpression(e analysis.Expression) Expression {
	out := Expression{Kind: e.Kind, Text: e.Text}
	for _, child := range e.Children {
		out.Children = append(out.Children, projectExpression(child))
	}
	return out
}

func projectStatement(s analysis.Statement, imports []FactImport, byStart map[int][]int) Statement {
	out := Statement{Kind: s.Kind, Name: s.Name, Line: s.Line, Target: projectExpression(s.Target), Value: projectExpression(s.Value)}
	// One statement can hold several imports sharing its start offset; attach
	// each projected import once.
	seen := map[int]bool{}
	for _, i := range s.Imports {
		for _, j := range byStart[i.Span.Start] {
			if !seen[j] {
				seen[j] = true
				out.Imports = append(out.Imports, imports[j])
			}
		}
	}
	for _, p := range s.Prelude {
		out.Prelude = append(out.Prelude, projectExpression(p))
	}
	for _, b := range s.Body {
		out.Body = append(out.Body, projectStatement(b, imports, byStart))
	}
	for _, e := range s.Else {
		out.Else = append(out.Else, projectStatement(e, imports, byStart))
	}
	return out
}

// View returns a fresh detached projection of the original extraction artifact.
func (f Facts) View() (Facts, error) {
	if f.raw == nil {
		return Facts{}, fmt.Errorf("facts must be produced by an Extractor")
	}
	return projectFacts(*f.raw)
}

// Digest identifies source bytes and material kind/version, independently of
// a graph snapshot. The logical path is available in View and is part of cache identity.
func (f Facts) Digest() [32]byte {
	if f.raw == nil {
		return [32]byte{}
	}
	return (Document{Path: f.raw.Path, Content: f.raw.Source, Gitlink: f.raw.Gitlink}).digest()
}
