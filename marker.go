package codegraph

type MarkerKind string

const (
	Spec MarkerKind = "spec"
	Case MarkerKind = "case"
	Rule MarkerKind = "rule"
	Link MarkerKind = "link"
	Doc  MarkerKind = "doc"
)

// Marker preserves the annotation payload and its source location.
// Structured case payloads are retained verbatim, not executed or interpreted.
type Marker struct {
	Kind     MarkerKind `json:"kind"`
	Text     string     `json:"text"`
	Location Location   `json:"location"`
}
