package codegraph

import "sort"

// Find returns declaration nodes in source order. An empty kind matches every
// declaration kind; an empty qualifiedName matches every name. File nodes are
// excluded because their name is a basename rather than a declaration name.
// The returned nodes are detached values and can be safely modified.
func (g *Graph) Find(path string, kind NodeKind, qualifiedName string) []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Node, 0)
	for _, node := range g.nodes {
		if node.Kind == File || node.Location.Path != path {
			continue
		}
		if kind != "" && node.Kind != kind {
			continue
		}
		if qualifiedName != "" && node.QualifiedName != qualifiedName {
			continue
		}
		out = append(out, cloneNode(node))
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
func (g *Graph) Node(id string) (Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	node, ok := g.nodes[id]
	if !ok {
		return Node{}, false
	}
	return cloneNode(node), true
}

// RelationsFrom and RelationsTo expose bounded adjacency without requiring a
// consumer to parse Cypher. If kinds is empty, all relation kinds are returned.
func (g *Graph) RelationsFrom(id string, kinds ...RelationKind) []Relation {
	return g.adjacent(id, false, kinds...)
}

func (g *Graph) RelationsTo(id string, kinds ...RelationKind) []Relation {
	return g.adjacent(id, true, kinds...)
}

func (g *Graph) adjacent(id string, incoming bool, kinds ...RelationKind) []Relation {
	allowed := make(map[RelationKind]bool, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	var relations []Relation
	for _, relation := range g.relations {
		if incoming && relation.Target != id || !incoming && relation.Source != id {
			continue
		}
		if len(allowed) > 0 && !allowed[relation.Kind] {
			continue
		}
		relations = append(relations, relation)
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
