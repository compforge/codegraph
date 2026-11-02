package codegraph

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/pkg"
)

type ManifestMetadata = pkg.ManifestMetadata

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
