package codegraph

import "github.com/compforge/codegraph/internal/confidence"

type RelationKind string

const (
	Contains   RelationKind = "contains"
	Declares   RelationKind = "declares"
	Imports    RelationKind = "imports"
	Calls      RelationKind = "calls"
	References RelationKind = "references"
	Extends    RelationKind = "extends"
	Implements RelationKind = "implements"
)

// Confidence describes evidence strength, not a calibrated probability.
type Confidence = confidence.Level

const (
	Exact     = confidence.Exact     // established by supported static semantics in the supplied snapshot
	Scoped    = confidence.Scoped    // constrained by bindings, imports, receivers or types
	NameOnly  = confidence.NameOnly  // name match without a proven binding
	Heuristic = confidence.Heuristic // convention or incomplete structural similarity
)

// Evidence records one derivation. Location, when present, points to supporting syntax.
type Evidence struct {
	Basis      string     `json:"basis"`
	Confidence Confidence `json:"confidence"`
	Location   *Location  `json:"location,omitempty"`
}

// Relation identifies one relation at one source location, including parallel calls.
type Relation struct {
	ID     string       `json:"id"`
	Source string       `json:"source"`
	Target string       `json:"target"`
	Kind   RelationKind `json:"kind"`
	// Confidence is the strongest independent Evidence confidence when published.
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
	Location   Location   `json:"location"`
}

// Path lists nodes in traversal order; Relations retain their stored direction.
type Path struct {
	Nodes     []Node     `json:"nodes"`
	Relations []Relation `json:"relations"`
}

type Subgraph struct {
	Nodes     []Node     `json:"nodes"`
	Relations []Relation `json:"relations"`
}

// cloneRelation detaches both the proof list and optional supporting locations.
func cloneRelation(r Relation) Relation {
	r.Evidence = append([]Evidence(nil), r.Evidence...)
	for i := range r.Evidence {
		if r.Evidence[i].Location != nil {
			loc := *r.Evidence[i].Location
			r.Evidence[i].Location = &loc
		}
	}
	return r
}
