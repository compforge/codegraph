package codegraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Document is one source unit or opaque gitlink supplied for graph construction.
// It may come from
// a filesystem, a Git revision, or memory; it need not exist on disk.
// Path is a slash-separated, snapshot-relative logical path (fs.ValidPath).
// It supplies identity, language detection, relative-import context, and source
// locations. Content is the complete source at that path, with offsets from zero;
// a gitlink instead supplies its pinned commit and has no source locations.
// Snapshot identity belongs to Graph. Each input is represented by a DocumentNodeKind
// node whose ID matches this document.
// +spec=`The same logical path identifies the same source unit across input batches`
type Document struct {
	Path    string
	Content []byte
	// Gitlink is the full pinned Git commit ID for an opaque submodule entry.
	// Set it only for Git mode 160000; Content must be empty. The caller reads
	// Git metadata. CodeGraph neither opens the checkout nor loads its symbols.
	Gitlink string
}

// Identifiable has a stable identity within one Graph snapshot.
type Identifiable interface {
	ID() string
}

// ID is the Document node identity for this source document.
func (d Document) ID() string { return DocumentID(d.Path) }

func (d Document) validate() error {
	if !fs.ValidPath(d.Path) {
		return fmt.Errorf("invalid source path %q", d.Path)
	}
	if d.Gitlink == "" {
		return nil
	}
	if d.Path == "." {
		return fmt.Errorf("gitlink must name an entry within the snapshot")
	}
	if len(d.Content) != 0 {
		return fmt.Errorf("gitlink %s cannot also contain source bytes", d.Path)
	}
	if len(d.Gitlink) != 40 && len(d.Gitlink) != 64 {
		return fmt.Errorf("gitlink %s requires a full commit object ID", d.Path)
	}
	if _, err := hex.DecodeString(d.Gitlink); err != nil {
		return fmt.Errorf("gitlink %s: invalid commit object ID", d.Path)
	}
	return nil
}

func (d Document) same(other Document) bool {
	return d.Gitlink == other.Gitlink && bytes.Equal(d.Content, other.Content)
}

func (d Document) matches(f analysis.Facts) bool {
	return d.Gitlink == f.Gitlink && bytes.Equal(d.Content, f.Source)
}

func (d Document) size() int64 { return int64(len(d.Content) + len(d.Gitlink)) }

func (d Document) digest() [32]byte {
	// Include the material type/version so a source or a different gitlink at
	// the same path can never reuse an extraction from another snapshot.
	h := sha256.New()
	h.Write([]byte(d.Gitlink))
	h.Write([]byte{0})
	h.Write(d.Content)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// A gitlink contributes one entry to the parent snapshot. Descendant materials
// belong to a different repository snapshot and cannot share that boundary.
func validateDocumentBoundaries(documents map[string]Document) error {
	names := make([]string, 0, len(documents))
	for p := range documents {
		names = append(names, p)
	}
	sort.Strings(names)
	for p, d := range documents {
		if d.Gitlink == "" {
			continue
		}
		prefix := p + "/"
		i := sort.SearchStrings(names, prefix)
		if i < len(names) && strings.HasPrefix(names[i], prefix) {
			return fmt.Errorf("gitlink %s is an opaque boundary; document %s belongs to its child snapshot", p, names[i])
		}
	}
	return nil
}

// Capabilities describes implemented extraction/resolution, not grammar availability.
// With no names it returns the language-specific adapters. Pass grammar names
// from Languages to inspect additional outline support without eagerly loading
// every registered grammar.
func Capabilities(languages ...string) []Capability {
	if len(languages) == 0 {
		languages = language.Registered()
	}
	var out []Capability
	for _, name := range languages {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil {
			continue
		}
		c := language.Lookup(entry.Name).Describe(*entry)
		cap := Capability{Language: c.Language, Limitations: c.Limitations, Documentation: c.Documentation, Entrypoints: c.Entrypoints}
		for _, v := range c.Organizations {
			cap.Organizations = append(cap.Organizations, NodeKind(v))
		}
		for _, v := range c.Declarations {
			cap.Declarations = append(cap.Declarations, NodeKind(v))
		}
		for _, v := range c.SourceItems {
			cap.SourceItems = append(cap.SourceItems, NodeKind(v))
		}
		for _, v := range c.References {
			cap.References = append(cap.References, ReferenceKind(v))
		}
		for _, v := range c.Relations {
			cap.Relations = append(cap.Relations, RelationKind(v))
		}
		for _, v := range c.Markers {
			cap.Markers = append(cap.Markers, MarkerKind(v))
		}
		out = append(out, cap)
	}
	return out
}

// Languages lists registered grammar names without loading their parsers.
// Availability is not a guarantee of extraction or semantic completeness.
func Languages() []string {
	entries := grammars.AllLanguages()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// Language returns the registered grammar name selected for a source path, or
// an empty string. Recognition does not imply complete semantic coverage.
func Language(name string) string {
	if entry := language.Detect(name); entry != nil {
		return entry.Name
	}
	return ""
}

type Capability struct {
	// Documentation reports support for declaration documentation. Language-specific
	// attachment rules and exclusions are described by Limitations.
	Documentation bool
	// Entrypoints reports support for language-native Node.Entrypoint recognition.
	Entrypoints bool
	// Organizations lists language units assembled from source contributions.
	Organizations []NodeKind
	Language      string
	Declarations  []NodeKind
	// SourceItems lists retained import/export item kinds, independent of binding.
	SourceItems []NodeKind
	// References lists emitted Node.ReferenceKind values, independently of target binding.
	References  []ReferenceKind
	Relations   []RelationKind
	Markers     []MarkerKind
	Limitations []string
}
