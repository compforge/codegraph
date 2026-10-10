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

// +why=Path structure must not use contains: that relation defines semantic namespace ownership.
// +spec=Every published Document has its ancestor directories, including parse failures and opaque gitlinks; tags match each path independently.
func (b *Builder) publishDocumentStructure(ctx context.Context, nodes map[string]Node, relations map[string]Relation) error {
	var documents []Node
	for _, n := range nodes {
		if n.Kind == DocumentNodeKind {
			documents = append(documents, n)
		}
	}
	evidence := 0
	for _, r := range relations {
		evidence += len(r.Evidence)
	}
	for _, n := range documents {
		n.Path = n.Location.Path
		n.Tags = b.tagMatcher.Match(n.Path)
		nodes[n.ID] = n
		child, parent := n.ID, path.Dir(n.Path)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := DirectoryID(parent)
			_, exists := nodes[id]
			if !exists {
				if len(nodes) >= b.opts.MaxNodes {
					return &BuildBudgetError{Stage: "directory", Resource: "MaxNodes", Used: len(nodes), Adding: 1, Limit: b.opts.MaxNodes}
				}
				nodes[id] = Node{ID: id, Kind: DirectoryNodeKind, Name: path.Base(parent), Path: parent, Tags: b.tagMatcher.Match(parent)}
			}
			edgeID := identity(child, id, InDirectory)
			if len(relations) >= b.opts.MaxRelations {
				return &BuildBudgetError{Stage: "directory", Resource: "MaxRelations", Used: len(relations), Adding: 1, Limit: b.opts.MaxRelations}
			}
			if evidence >= b.opts.MaxEvidence {
				return &BuildBudgetError{Stage: "directory", Resource: "MaxEvidence", Used: evidence, Adding: 1, Limit: b.opts.MaxEvidence}
			}
			relations[edgeID] = Relation{ID: edgeID, Source: child, Target: id, Kind: InDirectory,
				Confidence: Exact, Evidence: []Evidence{{Basis: "document_path", Confidence: Exact}}}
			evidence++
			// An existing directory already has its complete ancestor chain.
			if exists || parent == "." {
				break
			}
			child, parent = id, path.Dir(parent)
		}
	}
	return nil
}
