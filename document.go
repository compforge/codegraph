package codegraph

import (
	"context"
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

// Identifiable has a stable identity within one Graph snapshot.
type Identifiable interface {
	ID() string
}

// ID is the File node identity for this source document.
func (d Document) ID() string { return FileID(d.Path) }

// AddDocuments admits an explicit batch and starts background graph construction.
// The batch is validated before any document is admitted. It returns after
// submission; callers can use GetDocument or FindAsync for early facts, and
// Wait to wait for complete graph publication. Input bytes are copied.
func (g *Graph) AddDocuments(ctx context.Context, documents ...Document) error {
	_, err := g.enqueueDocuments(ctx, documents...)
	return err
}
