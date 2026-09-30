package codegraph

import (
	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/model"
)

// Document is one source unit or opaque gitlink supplied for graph construction.
// It may come from
// a filesystem, a Git revision, or memory; it need not exist on disk.
// Path is a slash-separated, snapshot-relative logical path (fs.ValidPath).
// It supplies identity, language detection, relative-import context, and source
// locations. Content is the complete source at that path, with offsets from zero;
// a gitlink instead supplies its pinned commit and has no source locations.
// Snapshot identity belongs to Graph. Each input is represented by a DocumentKind
// node whose ID matches this document.
// +spec=`The same logical path identifies the same source unit across input batches`
type Document = extract.Document

// Identifiable has a stable identity within one Graph snapshot.
type Identifiable = extract.Identifiable

// Facts owns the complete immutable extraction artifact for one document
// version. Copying it shares immutable material, including language-specific
// evidence needed for binding. Exported fields are detached inspection views:
// changing them does not change what Builder.Add consumes. Use View for a fresh
// projection. Only Extractor (or legacy Extract) can produce valid Facts.
type Facts = extract.Facts

// Statement preserves execution order and scope without evaluating code.
// Kind values are grammar node types of the capturing language.
type Statement = extract.Statement

type Expression = extract.Expression

type FactDeclaration = extract.FactDeclaration

type FactImport = extract.FactImport

// FactImportBinding preserves a source name, its local alias and its statement scope.
type FactImportBinding = extract.FactImportBinding

// FactReference records one identifier use. Owner indexes Declarations, or is
// -1 for file scope. A missing target does not discard the lexical fact.
type FactReference = extract.FactReference

// FactCallTarget records a syntax-based callable candidate before graph binding.
// Kind describes the callable form; Constructor candidates bind to Class nodes.
// Module is a source import qualifier, not a fetched dependency identity.
type FactCallTarget = extract.FactCallTarget

// FactTypeRelation records explicit inheritance/interface syntax. Owner indexes
// Declarations; Name and Module retain the unbound source spelling.
type FactTypeRelation = extract.FactTypeRelation

type FactCall = extract.FactCall

// ExtractionOptions controls parser work independently of any graph snapshot.
// Cache is optional and caller-owned; a Facts value remains usable after eviction.
type ExtractionOptions = extract.ExtractionOptions

// Extractor owns bounded parsing. It has no graph, binding or snapshot state.
// Extract and Submit may be called concurrently. No worker survives its task.
type Extractor = extract.Extractor

// ExtractionCache reuses detached single-document facts across Graphs. It never
// retains bound relationships or graph state. The caller owns its lifetime;
// keep the grammar registry unchanged while sharing it. It is safe for concurrent
// Graphs, but concurrent misses may extract independently.
// Capacity bounds retained document versions and their source bytes, not total
// heap usage. A full cache skips new entries without changing build behavior.
type ExtractionCache = extract.ExtractionCache

func NewExtractor(opts ExtractionOptions) (*Extractor, error) { return extract.NewExtractor(opts) }

// NewExtractionCache creates a bounded, in-memory extraction cache. Zero limits
// select 256 document versions and 32 MiB of source; negative limits are invalid.
func NewExtractionCache(maxDocuments int, maxSourceBytes int64) (*ExtractionCache, error) {
	return extract.NewExtractionCache(maxDocuments, maxSourceBytes)
}

// DocumentID identifies a document node by its snapshot-relative logical path.
func DocumentID(name string) string { return model.DocumentID(name) }
