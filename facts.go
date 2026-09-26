package codegraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"

	"github.com/compforge/codegraph/internal/extract"
)

// Facts is the detached extraction result for one source document, projected
// from the same facts AddDocuments consumes. Extraction succeeds without
// publishing any graph state, so consumers can explore dependencies and still
// hand the same documents to AddDocuments without a second parse.
type Facts struct {
	Path, Package, Language string
	Declarations            []FactDeclaration
	Imports                 []FactImport
	Calls                   []FactCall
	References              []FactReference
	Issues                  []Diagnostic
	// Exports maps explicit public names to local declarations or imported bindings.
	// Cross-module re-exports are recorded on Imports.Bindings.
	Exports map[string]string
	// Statements preserve execution order and scope with a bounded expression
	// vocabulary, for consumer-side interpretation; the graph model never
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
	Markers             []Marker
}

type FactImport struct {
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
	Name, Local         string
	Namespace, ReExport bool
	Location            Location
}

// FactReference records one identifier use. Owner indexes Declarations, or is
// -1 for file scope. A missing target does not discard the lexical fact.
type FactReference struct {
	Name, Receiver string
	Location       Location
	Owner          int
}

type FactCall struct {
	Name, Receiver string
	Location       Location
	// Blocked means syntax proves this is not a resolvable static function reference.
	Blocked, Builtin bool
}

// factCacheEntry keys extracted facts by content identity, so a cache hit
// cannot confuse two different sources sharing one logical path.
type factCacheEntry struct {
	hash  [32]byte
	facts extract.Facts
}

// Extract returns detached source facts for one document without publishing
// graph state. Results are cached by path and content identity: a later
// AddDocuments of the same path and content reuses them instead of parsing
// again, and facts of already loaded documents are projected without parsing.
// Documents without a registered grammar yield file-level facts carrying an
// unsupported_language issue, mirroring how AddDocuments records them.
// +spec=`Exploration extraction never reparses identical snapshot content`
func (g *Graph) Extract(ctx context.Context, document Document) (Facts, error) {
	if !fs.ValidPath(document.Path) {
		return Facts{}, fmt.Errorf("invalid source path %q", document.Path)
	}
	hash := sha256.Sum256(document.Content)
	g.factCacheMu.Lock()
	entry, cached := g.factCache[document.Path]
	g.factCacheMu.Unlock()
	if cached && entry.hash == hash {
		return projectFacts(entry.facts)
	}
	g.mu.RLock()
	staged, loaded := g.files[document.Path]
	g.mu.RUnlock()
	if loaded && sha256.Sum256(staged.Source) == hash {
		return projectFacts(staged)
	}
	var facts extract.Facts
	if extract.Detect(document.Path) == nil {
		facts = extract.FileOnly(document.Path, bytes.Clone(document.Content))
		facts.Issues = append(facts.Issues, extract.Issue{Code: "unsupported_language", Message: "no registered grammar for file", Subject: "document", Span: extract.Span{End: len(document.Content)}})
	} else {
		if parseObserver != nil {
			parseObserver(document.Path)
		}
		var err error
		facts, err = extract.Analyze(ctx, document.Path, bytes.Clone(document.Content), g.opts.ParseTimeout)
		if err != nil {
			return Facts{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}
	g.factCacheMu.Lock()
	g.factCache[document.Path] = factCacheEntry{hash: hash, facts: facts}
	g.factCacheMu.Unlock()
	return projectFacts(facts)
}

// cachedFacts returns a cached extraction for identical content and consumes
// the entry: once staged, the graph's retained facts become the authoritative
// copy, so the cache stays bounded to explored-but-not-yet-added documents.
func (g *Graph) cachedFacts(name string, data []byte) (extract.Facts, bool) {
	g.factCacheMu.Lock()
	defer g.factCacheMu.Unlock()
	entry, ok := g.factCache[name]
	if !ok || entry.hash != sha256.Sum256(data) {
		return extract.Facts{}, false
	}
	delete(g.factCache, name)
	return entry.facts, true
}

func projectFacts(f extract.Facts) (Facts, error) {
	out := Facts{Path: f.Path, Package: f.Package, Language: f.Language}
	for _, d := range f.Declarations {
		kind, err := declarationKind(d.Kind)
		if err != nil {
			return Facts{}, fmt.Errorf("%s: %w", f.Path, err)
		}
		decl := FactDeclaration{Name: d.Name, QualifiedName: d.QualifiedName, Kind: kind, Location: location(f, d.Span)}
		for _, m := range d.Comments {
			decl.Markers = append(decl.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
		}
		out.Declarations = append(out.Declarations, decl)
	}
	imports := make([]FactImport, 0, len(f.Imports))
	byStart := map[int][]int{}
	for j, i := range f.Imports {
		byStart[i.Span.Start] = append(byStart[i.Span.Start], j)
		bindings := make([]FactImportBinding, 0, len(i.Bindings))
		for _, b := range i.Bindings {
			bindings = append(bindings, FactImportBinding{Name: b.Name, Local: b.Local, Namespace: b.Namespace, ReExport: b.ReExport, Location: location(f, b.Span)})
		}
		imports = append(imports, FactImport{Bindings: bindings, Alias: i.Alias, Path: i.Path, From: i.From, Relative: i.Relative, Binding: i.Binding, Names: append([]string(nil), i.Names...), Location: location(f, i.Span)})
	}
	out.Imports = imports
	for _, s := range f.Python {
		out.Statements = append(out.Statements, projectStatement(s, imports, byStart))
	}
	for public, local := range f.Exports {
		if out.Exports == nil {
			out.Exports = map[string]string{}
		}
		out.Exports[public] = local
	}
	for _, c := range f.Calls {
		out.Calls = append(out.Calls, FactCall{Name: c.Name, Receiver: c.Receiver, Location: location(f, c.Span), Blocked: c.Blocked, Builtin: c.Builtin})
	}
	for _, r := range f.References {
		out.References = append(out.References, FactReference{Name: r.Name, Receiver: r.Receiver, Location: location(f, r.Span), Owner: r.Owner})
	}
	for _, issue := range f.Issues {
		out.Issues = append(out.Issues, extractionDiagnostic(f, issue))
	}
	return out, nil
}

func projectExpression(e extract.PythonExpression) Expression {
	out := Expression{Kind: e.Kind, Text: e.Text}
	for _, child := range e.Children {
		out.Children = append(out.Children, projectExpression(child))
	}
	return out
}

func projectStatement(s extract.PythonStatement, imports []FactImport, byStart map[int][]int) Statement {
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
