package codegraph

import (
	"context"
	"slices"

	"github.com/compforge/codegraph/internal/analysis"
)

type selectionReason uint8

const (
	coreSelection selectionReason = iota
	bridgeSelection
	contextSelection
)

type selection map[analysis.Ref]selectionReason

// entitySelector selects semantic entities without constructing public Nodes.
// +spec=Selection never changes binding facts or synthesizes dependency edges.
// +why=Bridge reachability is admission policy over semantic Refs, not another graph query engine.
// +link=component://docs/admission.md
type entitySelector struct {
	index        *analysis.Index
	policy       *AdmissionPolicy
	selected     selection
	local        map[analysis.Ref]bool
	steps, limit int
}

func permits(p *AdmissionPolicy, kind NodeKind) bool {
	return p == nil || p.NodeKinds == nil || slices.Contains(p.NodeKinds, kind)
}
func (s *entitySelector) step(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.steps >= s.limit {
		return &BuildBudgetError{Stage: "selection", Resource: "SelectionSteps", Used: s.steps, Adding: 1, Limit: s.limit}
	}
	s.steps++
	return nil
}
func (s *entitySelector) Select(ctx context.Context) (selection, error) {
	s.selected = selection{}
	s.local = map[analysis.Ref]bool{}
	if s.policy != nil && (s.policy.GlobalVariablesOnly || s.policy.IncludeDependencyBridges) {
		for name, f := range s.index.Files {
			scopes := map[int]int{}
			if f.Lexical != nil {
				for _, b := range f.Lexical.Bindings {
					if _, ok := scopes[b.Target]; !ok {
						scopes[b.Target] = b.Scope
					}
				}
			}
			for i, d := range f.Declarations {
				if err := s.step(ctx); err != nil {
					return nil, err
				}
				kind := NodeKind(analysis.ConcreteKind(d.Kind))
				if (kind == Variable || kind == Constant) && !d.Callable && !globalDeclaration(f, i, scopes) {
					s.local[analysis.DeclarationRef(name, i)] = true
				}
			}
		}
	}
	for ref, e := range s.index.Entities {
		if err := s.step(ctx); err != nil {
			return nil, err
		}
		if !permits(s.policy, NodeKind(e.Kind)) || (s.policy != nil && s.policy.GlobalVariablesOnly && s.local[ref]) {
			continue
		}
		reason := coreSelection
		switch NodeKind(e.Kind) {
		case DocumentNodeKind, Module, Package, Namespace, Import, Export, Reference:
			reason = contextSelection
		}
		s.selected[ref] = reason
	}
	if s.policy != nil && s.policy.IncludeDependencyBridges {
		if err := s.bridges(ctx); err != nil {
			return nil, err
		}
	}
	return s.selected, nil
}

// bridges retains the existing bounded two-label admission algorithm over Refs.
// Structural ownership can enter a candidate; only dependency edges can leave it.
func (s *entitySelector) bridges(ctx context.Context) error {
	anchors, candidates := map[analysis.Ref]bool{}, map[analysis.Ref]bool{}
	for id, n := range s.index.Entities {
		if _, ok := s.selected[id]; ok {
			switch NodeKind(n.Kind) {
			case DocumentNodeKind, DirectoryNodeKind, Module, Package, Namespace, Import, Export, Reference:
			default:
				anchors[id] = true
			}
		} else if s.local[id] || NodeKind(n.Kind) == Field || NodeKind(n.Kind) == Property {
			candidates[id] = true
		}
	}
	// A resource Document is a dependency endpoint for a local resource initializer.
	// Other document context remains outside dependency bridge admission.
	for _, edge := range s.index.Edges {
		if err := s.step(ctx); err != nil {
			return err
		}
		if edge.Kind == string(References) && edge.Target.IsDocument() {
			if _, kept := s.selected[edge.Target]; kept {
				anchors[edge.Target] = true
			}
		}
	}
	forward, backward := map[analysis.Ref][]analysis.Ref{}, map[analysis.Ref][]analysis.Ref{}
	for _, r := range s.index.Edges {
		if err := s.step(ctx); err != nil {
			return err
		}
		if !(anchors[r.Source] || candidates[r.Source]) || !(anchors[r.Target] || candidates[r.Target]) {
			continue
		}
		switch RelationKind(r.Kind) {
		case Calls, References, Aliases, Extends, Implements:
		case Encloses, Contains:
			if !anchors[r.Source] || !candidates[r.Target] {
				continue
			}
		default:
			continue
		}
		forward[r.Source] = append(forward[r.Source], r.Target)
		backward[r.Target] = append(backward[r.Target], r.Source)
	}
	// Two distinct labels suffice to establish different endpoints. Each candidate
	// propagates at most twice in each direction, including in cyclic graphs.
	reach := func(adjacency map[analysis.Ref][]analysis.Ref) (map[analysis.Ref][]analysis.Ref, error) {
		type step struct{ node, anchor analysis.Ref }
		labels := map[analysis.Ref][]analysis.Ref{}
		var queue []step
		add := func(node, anchor analysis.Ref) {
			if candidates[node] && len(labels[node]) < 2 && !slices.Contains(labels[node], anchor) {
				labels[node] = append(labels[node], anchor)
				queue = append(queue, step{node, anchor})
			}
		}
		for anchor := range anchors {
			for _, next := range adjacency[anchor] {
				if err := s.step(ctx); err != nil {
					return nil, err
				}
				add(next, anchor)
			}
		}
		for i := 0; i < len(queue); i++ {
			if err := s.step(ctx); err != nil {
				return nil, err
			}
			for _, next := range adjacency[queue[i].node] {
				if err := s.step(ctx); err != nil {
					return nil, err
				}
				add(next, queue[i].anchor)
			}
		}
		return labels, nil
	}
	from, err := reach(forward)
	if err != nil {
		return err
	}
	to, err := reach(backward)
	if err != nil {
		return err
	}
	for id := range candidates {
		for _, source := range from[id] {
			for _, target := range to[id] {
				if source != target {
					s.selected[id] = bridgeSelection
				}
			}
		}
	}
	return nil
}
