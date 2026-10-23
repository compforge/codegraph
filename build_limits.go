package codegraph

import "fmt"

// AnalysisLimits bounds semantic work independently of the published graph.
// Zero values use finite defaults; Node/Relation budgets only charge publication.
// +spec=Omitting use nodes never removes the budget on their analysis.
// +link=component://docs/admission.md
type AnalysisLimits struct {
	Entities       int // Documents, declarations and language-owned synthetic entities.
	Uses           int // Call, reference and type-use records in admitted Facts.
	Relations      int // Distinct internal relation occurrences.
	Evidence       int // Internal proofs; remaining capacity also bounds candidate expansion.
	SelectionSteps int // Entity selection and bridge propagation work.
}

func (l *AnalysisLimits) defaults() error {
	for _, p := range []struct {
		value    *int
		fallback int
	}{{&l.Entities, 250000}, {&l.Uses, 1000000}, {&l.Relations, 1000000}, {&l.Evidence, 2000000}, {&l.SelectionSteps, 10000000}} {
		if *p.value < 0 {
			return fmt.Errorf("analysis limits must not be negative")
		}
		if *p.value == 0 {
			*p.value = p.fallback
		}
	}
	return nil
}
