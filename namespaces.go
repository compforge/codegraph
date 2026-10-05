package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// NamespaceOptions selects returned namespace kinds and the minimum confidence
// of every traversed relation. Empty Kinds includes all namespace roles (including
// types organizing members); MinConfidence defaults to Exact. Kinds filters the
// result, not intermediate owners. QueryTimeout and graph query budgets apply.
type NamespaceOptions struct {
	Kinds         []NodeKind
	MinConfidence Confidence
}

// NamespaceMatch is a detached namespace and its shortest ownership distance.
// For CommonNamespaces, Depth is the maximum shortest distance across inputs.
// Depth counts relations, including a Document's contribution to its source root.
type NamespaceMatch struct {
	Node  Node `json:"node"`
	Depth int  `json:"depth"`
}

// NamespaceAncestors returns namespaces in increasing distance, then ID order.
// A namespace includes itself at depth zero. Membership follows incoming contains;
// Documents start at their contributed synthetic source roots. Reference, Import
// and Export nodes follow occurs_in to their source context.
// Aliases and reference targets never change source ownership. Unknown IDs and
// absent ownership yield no matches. No partial matches accompany an error.
// +spec=Namespace navigation derives only from Node and Relation, never paths, Facts or parser artifacts.
func (g *Graph) NamespaceAncestors(ctx context.Context, id string, opts NamespaceOptions) ([]NamespaceMatch, error) {
	return g.CommonNamespaces(ctx, []string{id}, opts)
}

// CommonNamespaces intersects the namespace ancestors of every supplied node.
// Empty input or any unknown node yields no matches; inputs are never dropped.
// All common namespaces are returned, nearest first, without choosing a business
// grouping boundary. Partial graphs can have several roots or no common root.
func (g *Graph) CommonNamespaces(ctx context.Context, ids []string, opts NamespaceOptions) ([]NamespaceMatch, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.MinConfidence == "" {
		opts.MinConfidence = Exact
	}
	if !opts.MinConfidence.Valid() {
		return nil, fmt.Errorf("namespace query: invalid confidence %q", opts.MinConfidence)
	}
	for _, kind := range opts.Kinds {
		if !namespaceKind(kind) {
			return nil, fmt.Errorf("namespace query: %q is not a namespace kind", kind)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	for _, id := range ids {
		if _, ok := g.nodes[id]; !ok {
			return nil, nil
		}
	}
	parents, err := g.namespaceParents(ctx, opts.MinConfidence)
	if err != nil {
		return nil, err
	}
	var common map[string]int
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		ancestors, err := g.namespaceDistances(ctx, id, parents)
		if err != nil {
			return nil, err
		}
		if common == nil {
			common = ancestors
			continue
		}
		for ancestor, depth := range common {
			other, ok := ancestors[ancestor]
			if !ok {
				delete(common, ancestor)
			} else if other > depth {
				common[ancestor] = other
			}
		}
	}
	allowed := map[NodeKind]bool{}
	for _, kind := range opts.Kinds {
		allowed[kind] = true
	}
	var out []NamespaceMatch
	var size int64
	for id, depth := range common {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		node := g.nodes[id]
		if len(allowed) > 0 && !allowed[node.Kind] {
			continue
		}
		match := NamespaceMatch{Node: cloneNode(node), Depth: depth}
		encoded, err := json.Marshal(match)
		if err != nil {
			return nil, err
		}
		size += int64(len(encoded))
		if len(out) >= g.limits.Rows || size > g.limits.Bytes {
			return nil, fmt.Errorf("namespace query results: %w", ErrQueryBudget)
		}
		out = append(out, match)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Node.ID < out[j].Node.ID
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func namespaceKind(kind NodeKind) bool {
	switch kind {
	case Package, Module, Namespace, Class, Struct, Interface, Type, Enum, Record, Trait, Union:
		return true
	}
	return false
}

// Build adjacency once per query. Source context for an import or use is not a
// contains fact; following these edges is a query operation, not a graph mutation.
func (g *Graph) namespaceParents(ctx context.Context, minimum Confidence) (map[string][]string, error) {
	parents := map[string][]string{}
	for _, r := range g.relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !r.Confidence.AtLeast(minimum) {
			continue
		}
		source, target := g.nodes[r.Source], g.nodes[r.Target]
		switch r.Kind {
		case Contains:
			parents[target.ID] = append(parents[target.ID], source.ID)
		case OccursIn:
			if source.Kind == Reference || source.Kind == Import || source.Kind == Export {
				parents[source.ID] = append(parents[source.ID], target.ID)
			}
		case Declares:
			if source.Kind != DocumentKind {
				continue
			}
			// A class declared in a document is its member, not the document's
			// owner. Only synthetic source roots establish document organization.
			if namespaceKind(target.Kind) && target.Location == nil {
				parents[source.ID] = append(parents[source.ID], target.ID)
			}
		}
	}
	return parents, nil
}

func (g *Graph) namespaceDistances(ctx context.Context, id string, parents map[string][]string) (map[string]int, error) {
	distance := map[string]int{id: 0}
	queue := []string{id}
	out := map[string]int{}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := queue[i]
		depth := distance[current]
		if namespaceKind(g.nodes[current].Kind) {
			out[current] = depth
		}
		for _, parent := range parents[current] {
			if _, visited := distance[parent]; visited {
				continue
			}
			if depth >= g.limits.Hops {
				return nil, fmt.Errorf("namespace query ancestry exceeds %d hops: %w", g.limits.Hops, ErrQueryBudget)
			}
			distance[parent] = depth + 1
			queue = append(queue, parent)
		}
	}
	return out, nil
}
