package graphstore

import (
	"sort"

	"github.com/compforge/codegraph/internal/graphmodel"
)

// Find returns declaration nodes in source order. An empty kind matches every
// declaration kind; an empty qualifiedName matches every name. Documents and
// organizations without a single Location are excluded. Follow declares from a
// Document to find its package/module contributions.
// The returned nodes are detached values and can be safely modified.
func (g *Snapshot) Find(path string, kind graphmodel.NodeKind, qualifiedName string) []graphmodel.Node {
	out := make([]graphmodel.Node, 0)
	for _, node := range g.nodes {
		if node.Kind == graphmodel.DocumentKind || node.Location == nil || node.Location.Path != path {
			continue
		}
		if kind != "" && node.Kind != kind {
			continue
		}
		if qualifiedName != "" && node.QualifiedName != qualifiedName {
			continue
		}
		out = append(out, graphmodel.CloneNode(node))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Location.StartByte != out[j].Location.StartByte {
			return out[i].Location.StartByte < out[j].Location.StartByte
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Node returns a detached node by its source identity.
func (g *Snapshot) Node(id string) (graphmodel.Node, bool) {
	node, ok := g.nodes[id]
	if !ok {
		return graphmodel.Node{}, false
	}
	return graphmodel.CloneNode(node), true
}

// RelationsFrom and RelationsTo expose bounded adjacency without requiring a
// consumer to parse Cypher. If kinds is empty, all relation kinds are returned.
func (g *Snapshot) RelationsFrom(id string, kinds ...graphmodel.RelationKind) []graphmodel.Relation {
	return g.adjacent(id, false, kinds...)
}

func (g *Snapshot) RelationsTo(id string, kinds ...graphmodel.RelationKind) []graphmodel.Relation {
	return g.adjacent(id, true, kinds...)
}

func (g *Snapshot) adjacent(id string, incoming bool, kinds ...graphmodel.RelationKind) []graphmodel.Relation {
	allowed := make(map[graphmodel.RelationKind]bool, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = true
	}
	var relations []graphmodel.Relation
	for _, relation := range g.relations {
		if incoming && relation.Target != id || !incoming && relation.Source != id {
			continue
		}
		if len(allowed) > 0 && !allowed[relation.Kind] {
			continue
		}
		relations = append(relations, graphmodel.CloneRelation(relation))
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Location.Path != relations[j].Location.Path {
			return relations[i].Location.Path < relations[j].Location.Path
		}
		if relations[i].Location.StartByte != relations[j].Location.StartByte {
			return relations[i].Location.StartByte < relations[j].Location.StartByte
		}
		return relations[i].ID < relations[j].ID
	})
	return relations
}
