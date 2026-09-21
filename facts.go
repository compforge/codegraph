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
	Issues                  []Diagnostic
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
	Location          Location
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
		facts.Issues = append(facts.Issues, extract.Issue{Code: "unsupported_language", Message: "no registered grammar for file"})
	} else {
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
	for _, i := range f.Imports {
		out.Imports = append(out.Imports, FactImport{Alias: i.Alias, Path: i.Path, From: i.From, Relative: i.Relative, Location: location(f, i.Span)})
	}
	for _, c := range f.Calls {
		out.Calls = append(out.Calls, FactCall{Name: c.Name, Receiver: c.Receiver, Location: location(f, c.Span), Blocked: c.Blocked, Builtin: c.Builtin})
	}
	for _, issue := range f.Issues {
		out.Issues = append(out.Issues, Diagnostic{Code: issue.Code, Message: issue.Message, Location: location(f, issue.Span)})
	}
	return out, nil
}
