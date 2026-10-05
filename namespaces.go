package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// NamespaceOptions filters returned kinds and requires a minimum confidence on
// every traversed relation (Exact by default). Kinds does not filter intermediate
// owners or prescribe a language hierarchy. Empty Kinds returns all namespace
// roles, including graph member owners. Graph query budgets and timeout apply.
type NamespaceOptions struct {
	Kinds         []NodeKind
	MinConfidence Confidence
}

// NamespaceMatch includes one shortest accepted path per distinct input, in input
// order. Paths start at the input and end at Node; relations retain stored direction
// and evidence. Among equal-length paths, stronger evidence wins, then stable IDs.
// Depth is the maximum path length; Confidence is the weakest edge across these
// returned proofs (Exact for identity paths), not the strongest of all possible
// longer paths. Raise MinConfidence when only stronger proofs are acceptable.
type NamespaceMatch struct {
	Node       Node       `json:"node"`
	Depth      int        `json:"depth"`
	Confidence Confidence `json:"confidence"`
	Paths      []Path     `json:"paths"`
}

// NamespaceAncestors returns namespaces in increasing distance, then ID order.
// A namespace includes itself at depth zero. Documents follow in_namespace;
// source uses follow occurs_in, and members follow incoming contains. No file-path
// or source-location convention determines ownership. Unknown IDs and absent
// ownership yield no matches. No partial matches accompany an error.
// +spec=Namespace navigation derives only from Node and Relation, never paths, Facts or parser artifacts.
func (g *Graph) NamespaceAncestors(ctx context.Context, id string, opts NamespaceOptions) ([]NamespaceMatch, error) {
	return g.CommonNamespaces(ctx, []string{id}, opts)
}

// CommonNamespaces intersects the ancestors of all inputs. Empty input or any
// unknown ID yields no matches; inputs are never dropped. Partial graphs may have
// several roots or no common root. Every common namespace is returned; choosing
// the grouping boundary remains a consumer decision.
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
	if len(ids) == 0 {
		return nil, nil
	}
	var inputs []string
	seen := map[string]bool{}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := g.nodes[id]; !ok {
			return nil, nil
		}
		if !seen[id] {
			inputs = append(inputs, id)
			seen[id] = true
		}
	}
	index, err := g.namespaces(ctx)
	if err != nil {
		return nil, err
	}
	visits := make([]map[string]namespaceVisit, 0, len(inputs))
	var common map[string]int
	for _, id := range inputs {
		found, err := g.namespaceVisits(ctx, id, index, opts.MinConfidence)
		if err != nil {
			return nil, err
		}
		visits = append(visits, found)
		if common == nil {
			common = map[string]int{}
			for owner, v := range found {
				if index.owners[owner] {
					common[owner] = v.depth
				}
			}
		} else {
			for owner, depth := range common {
				other, ok := found[owner]
				if !ok {
					delete(common, owner)
				} else {
					common[owner] = max(depth, other.depth)
				}
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
		if len(out) >= g.limits.Rows {
			return nil, fmt.Errorf("namespace query results: %w", ErrQueryBudget)
		}
		match := NamespaceMatch{Node: cloneNode(node), Depth: depth, Confidence: Exact}
		// Charge each proof before retaining it, so large multi-input intersections
		// cannot allocate an unbounded result before checking the byte budget.
		for _, found := range visits {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			path := g.namespacePath(id, found)
			encoded, err := json.Marshal(path)
			if err != nil {
				return nil, err
			}
			size += int64(len(encoded)) + 1
			if size > g.limits.Bytes {
				return nil, fmt.Errorf("namespace query results: %w", ErrQueryBudget)
			}
			match.Paths = append(match.Paths, path)
			match.Confidence = match.Confidence.Weaker(found[id].confidence)
		}
		header := match
		header.Paths = nil
		encoded, err := json.Marshal(header)
		if err != nil {
			return nil, err
		}
		size += int64(len(encoded))
		if size > g.limits.Bytes {
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

type namespaceVisit struct {
	depth      int
	confidence Confidence
	previous   string
	relation   string
}

func (g *Graph) namespaceVisits(ctx context.Context, id string, index *namespaceIndex, minimum Confidence) (map[string]namespaceVisit, error) {
	found := map[string]namespaceVisit{id: {confidence: Exact}}
	queue := []string{id}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := queue[i]
		visit := found[current]
		for _, step := range index.parents[current] {
			edge := g.relations[step.relation]
			if !edge.Confidence.AtLeast(minimum) {
				continue
			}
			candidate := namespaceVisit{visit.depth + 1, visit.confidence.Weaker(edge.Confidence), current, step.relation}
			old, exists := found[step.parent]
			if exists && old.depth < candidate.depth {
				continue
			}
			if visit.depth >= g.limits.Hops {
				return nil, fmt.Errorf("namespace query ancestry exceeds %d hops: %w", g.limits.Hops, ErrQueryBudget)
			}
			if !exists {
				found[step.parent] = candidate
				queue = append(queue, step.parent)
			} else if candidate.confidence != old.confidence && candidate.confidence.AtLeast(old.confidence) || candidate.confidence == old.confidence && (candidate.previous < old.previous || candidate.previous == old.previous && candidate.relation < old.relation) {
				// BFS settles every predecessor layer before this node is expanded, so
				// a stronger equal-length proof propagates to all subsequent ancestors.
				found[step.parent] = candidate
			}
		}
	}
	return found, nil
}

func (g *Graph) namespacePath(id string, found map[string]namespaceVisit) Path {
	var reverse []string
	for current := id; current != ""; current = found[current].previous {
		reverse = append(reverse, current)
	}
	path := Path{Nodes: make([]Node, 0, len(reverse)), Relations: make([]Relation, 0, len(reverse)-1)}
	for i := len(reverse) - 1; i >= 0; i-- {
		path.Nodes = append(path.Nodes, cloneNode(g.nodes[reverse[i]]))
		if i < len(reverse)-1 {
			path.Relations = append(path.Relations, cloneRelation(g.relations[found[reverse[i]].relation]))
		}
	}
	return path
}
