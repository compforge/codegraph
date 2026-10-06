package codegraph

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
)

// DocumentKind describes material, independently of its parsing grammar.
// A Manifest is not evidence of an independent engineering Component.
type DocumentKind string

const (
	SourceDocument   DocumentKind = "source"
	ManifestDocument DocumentKind = "manifest"
	GitlinkDocument  DocumentKind = "gitlink"
	UnknownDocument  DocumentKind = "unknown"
)

// ManifestMetadata preserves explicit project declarations. Name is a Go module
// path or a packaging project name; Python/JS names are not import namespaces.
// Absent metadata does not imply that a project lacks a dynamically supplied value.
type ManifestMetadata struct {
	Format          string    `json:"format"`
	Name            string    `json:"name,omitempty"`
	Version         string    `json:"version,omitempty"`
	Project         bool      `json:"project"`
	BuildSystem     bool      `json:"buildSystem"`
	Workspace       bool      `json:"workspace"`
	NameLocation    *Location `json:"nameLocation,omitempty"`
	VersionLocation *Location `json:"versionLocation,omitempty"`
}

// DocumentNode is a typed view of the same Node stored in Graph. Declaration
// membership and outlines come from relations, never a second object hierarchy.
type DocumentNode struct{ Node }

// Document returns a detached Document node by snapshot-relative path.
func (g *Graph) Document(path string) (DocumentNode, bool) {
	n, ok := g.Node(DocumentID(path))
	if !ok || n.Kind != DocumentNodeKind {
		return DocumentNode{}, false
	}
	return DocumentNode{Node: n}, true
}

func projectManifest(f analysis.Facts) *ManifestMetadata {
	if f.Manifest == nil {
		return nil
	}
	m := f.Manifest
	out := &ManifestMetadata{Format: m.Format, Name: m.Name, Version: m.Version, Project: m.Project, BuildSystem: m.BuildSystem, Workspace: m.Workspace}
	if m.NameSpan.End > m.NameSpan.Start {
		out.NameLocation = locationPtr(f, m.NameSpan)
	}
	if m.VersionSpan.End > m.VersionSpan.Start {
		out.VersionLocation = locationPtr(f, m.VersionSpan)
	}
	return out
}

func cloneManifest(m *ManifestMetadata) *ManifestMetadata {
	if m == nil {
		return nil
	}
	out := *m
	if m.NameLocation != nil {
		loc := *m.NameLocation
		out.NameLocation = &loc
	}
	if m.VersionLocation != nil {
		loc := *m.VersionLocation
		out.VersionLocation = &loc
	}
	return &out
}

func materialKind(path, gitlink string) DocumentKind {
	return DocumentKind(language.MaterialKind(path, gitlink))
}
