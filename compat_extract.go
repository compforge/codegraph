package codegraph

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

// factCacheEntry keys extracted facts by content identity, so a cache hit
// cannot confuse two different sources sharing one logical path.
type factCacheEntry struct {
	hash  [32]byte
	facts analysis.Facts
}

// Extract returns detached source facts for one document without publishing
// graph state. Results are cached by path and content identity: a later
// AddDocuments of the same path and content reuses them instead of parsing
// again, and facts of already loaded documents are projected without parsing.
// Source documents without a registered grammar yield file-level facts carrying an
// unsupported_language issue, mirroring how AddDocuments records them.
// Gitlinks yield only their path and commit, without attempting language parsing.
// +spec=`Exploration extraction never reparses identical snapshot content`
func (g *Builder) Extract(ctx context.Context, document Document) (Facts, error) {
	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}
	if err := document.validate(); err != nil {
		return Facts{}, err
	}
	hash := document.digest()
	g.factCacheMu.Lock()
	entry, cached := g.factCache[document.Path]
	g.factCacheMu.Unlock()
	if cached && entry.hash == hash {
		return projectFacts(entry.facts)
	}
	g.mu.RLock()
	staged, loaded := g.documents[document.Path]
	g.mu.RUnlock()
	if loaded && document.matches(staged) {
		return projectFacts(staged)
	}
	facts, err := g.extractor.extract(ctx, document)
	if err != nil {
		return Facts{}, err
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
func (g *Builder) cachedFacts(document Document) (analysis.Facts, bool) {
	g.factCacheMu.Lock()
	defer g.factCacheMu.Unlock()
	entry, ok := g.factCache[document.Path]
	if !ok || entry.hash != document.digest() {
		return analysis.Facts{}, false
	}
	delete(g.factCache, document.Path)
	return entry.facts, true
}
