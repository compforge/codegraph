package codegraph

import (
	"bytes"
	"context"
	"fmt"
)

// Document is one source unit supplied for graph construction. It may come from
// a filesystem, a Git revision, or memory; it need not exist on disk.
// Path is a slash-separated, snapshot-relative logical path (fs.ValidPath).
// It supplies identity, language detection, relative-import context, and source
// locations. Content is the complete source at that path, with offsets from zero.
// Snapshot identity belongs to Graph. A Document is input, not a node kind.
// +spec=`The same logical path identifies the same source unit across input batches`
type Document struct {
	Path    string
	Content []byte
}

// AddDocuments adds an explicit batch of source documents atomically. Content
// must remain unchanged during the call; the graph retains its own copy afterward.
// Scope and all size/parse budgets apply to every explicit batch. Re-adding a
// loaded path with different content returns ErrSnapshotChanged.
// Identical paths and content within a batch are deduplicated; conflicting
// content returns ErrSnapshotChanged. Unsupported or unparseable documents
// produce partial coverage; budget, identity, and cancellation errors roll back.
// References resolve against this batch and previously loaded documents.
// Dependencies are never fetched implicitly; callers supply them explicitly.
func (g *Graph) AddDocuments(ctx context.Context, documents ...Document) (BuildReport, error) {
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	byPath := make(map[string]Document, len(documents))
	paths := make([]string, 0, len(documents))
	for _, document := range documents {
		if prior, ok := byPath[document.Path]; ok {
			if !bytes.Equal(prior.Content, document.Content) {
				return g.Report(), fmt.Errorf("%w: %s", ErrSnapshotChanged, document.Path)
			}
			continue
		}
		byPath[document.Path] = document
		paths = append(paths, document.Path)
	}
	ordered := make([]Document, 0, len(paths))
	for _, path := range paths {
		ordered = append(ordered, byPath[path])
	}
	return g.add(ctx, ordered...)
}
