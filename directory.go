package codegraph

import (
	"context"
	"path"
)

// DirectoryID identifies a directory by its snapshot-relative path; "." is root.
func DirectoryID(path string) string { return "directory:" + path }

// DirectoryNode is a detached typed view of the same Directory node in Graph.
// Its existence records an ancestor of supplied material, not complete membership.
type DirectoryNode struct{ Node }

func (g *Graph) Directory(path string) (DirectoryNode, bool) {
	n, ok := g.Node(DirectoryID(path))
	if !ok || n.Kind != DirectoryNodeKind {
		return DirectoryNode{}, false
	}
	return DirectoryNode{Node: n}, true
}

// +why=Path structure is independent of semantic member ownership and admission.
func (a *graphAssembler) publishDocumentStructure(ctx context.Context, failures map[string]Diagnostic) error {
	a.stage = "directory"
	documents := map[string]string{}
	for p, f := range a.files {
		documents[p] = f.Gitlink
	}
	for p, d := range failures {
		if d.Code == "parse_error" {
			documents[p] = ""
		}
	}
	for p, gitlink := range documents {
		docID := DocumentID(p)
		if n, ok := a.nodes[docID]; ok {
			n.Path = p
			n.Tags = a.tags.DocumentTags(Document{Path: p, Gitlink: gitlink})
			if err := a.addNode(n); err != nil {
				return err
			}
		}
		if !permits(a.opts.Admission, DirectoryNodeKind) {
			continue
		}
		child, parent := docID, path.Dir(p)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := DirectoryID(parent)
			_, exists := a.nodes[id]
			if !exists {
				if err := a.addNode(Node{ID: id, Kind: DirectoryNodeKind, Name: path.Base(parent), Path: parent, Tags: a.tags.Match(parent)}); err != nil {
					return err
				}
			}
			if err := a.addRelation(Relation{ID: identity(child, id, InDirectory), Source: child, Target: id, Kind: InDirectory, Confidence: Exact, Evidence: []Evidence{{Basis: "document_path", Confidence: Exact}}}); err != nil {
				return err
			}
			if exists || parent == "." {
				break
			}
			child, parent = id, path.Dir(parent)
		}
	}
	return nil
}
