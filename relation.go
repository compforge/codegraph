package codegraph

type RelationKind string

const (
	Contains   RelationKind = "contains"
	Imports    RelationKind = "imports"
	Calls      RelationKind = "calls"
	References RelationKind = "references"
	Extends    RelationKind = "extends"
	Implements RelationKind = "implements"
)

// Confidence describes evidence strength, not a calibrated probability.
type Confidence string

const (
	Exact     Confidence = "exact"     // syntactically owned or uniquely bound within the supplied scope
	Candidate Confidence = "candidate" // a possible target without sufficient binding evidence
)

// Relation identifies one relation at one source location, including parallel calls.
type Relation struct {
	ID         string       `json:"id"`
	Source     string       `json:"source"`
	Target     string       `json:"target"`
	Kind       RelationKind `json:"kind"`
	Confidence Confidence   `json:"confidence"`
	Basis      string       `json:"basis"`
	Location   Location     `json:"location"`
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
