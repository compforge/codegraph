package codegraph

import "github.com/compforge/codegraph/internal/extract"

// DiagnosticSubject identifies the information a local gap concerns. It does
// not prescribe whether a consumer should expand its analysis or reject a path.
type DiagnosticSubject string

const (
	DocumentSubject     DiagnosticSubject = "document"
	DeclarationsSubject DiagnosticSubject = "declarations"
	RelationsSubject    DiagnosticSubject = "relations"
	ContextSubject      DiagnosticSubject = "context"
	ResourcesSubject    DiagnosticSubject = "resources"
)

// Diagnostic records information that could not be produced. Candidate edges
// already carry their uncertainty in Confidence and Basis.
// Location covers the whole document when the producer cannot localize the gap.
type Diagnostic struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Subject  DiagnosticSubject `json:"subject"`
	Relation RelationKind      `json:"relation,omitempty"`
	Location Location          `json:"location"`
	Outline  *OutlineCoverage  `json:"outline,omitempty"`
}

// OutlineCoverage counts query candidates, not all declarations in the source.
// Zero omissions do not prove language coverage. The upstream outline API has
// no omission locations, so these counts apply to the diagnostic's document.
type OutlineCoverage struct {
	Symbols                    int    `json:"symbols"`
	OmittedNoName              int    `json:"omittedNoName,omitempty"`
	OmittedDuplicate           int    `json:"omittedDuplicate,omitempty"`
	OmittedNameConflict        int    `json:"omittedNameConflict,omitempty"`
	OmittedConflict            int    `json:"omittedConflict,omitempty"`
	OmittedOverlap             int    `json:"omittedOverlap,omitempty"`
	OmittedInvalidNameRange    int    `json:"omittedInvalidNameRange,omitempty"`
	OmittedMultipleDefinitions int    `json:"omittedMultipleDefinitions,omitempty"`
	OwnerRuleMisses            int    `json:"ownerRuleMisses,omitempty"`
	DeclineReason              string `json:"declineReason,omitempty"`
	Truncated                  bool   `json:"truncated,omitempty"`
}

func extractionDiagnostic(f extract.Facts, issue extract.Issue) Diagnostic {
	d := Diagnostic{Code: issue.Code, Message: issue.Message, Subject: DiagnosticSubject(issue.Subject),
		Relation: RelationKind(issue.Relation), Location: location(f, issue.Span)}
	if issue.Outline != nil {
		outline := OutlineCoverage(*issue.Outline)
		d.Outline = &outline
	}
	return d
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), in...)
	for i := range out {
		if out[i].Outline != nil {
			outline := *out[i].Outline
			out[i].Outline = &outline
		}
	}
	return out
}
