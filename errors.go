package codegraph

import (
	"errors"
	"fmt"

	"github.com/compforge/codegraph/internal/graphstore"
)

var (
	// ErrSnapshotChanged indicates different content for an already admitted path.
	ErrSnapshotChanged = errors.New("source changed within graph snapshot")
	// ErrDocumentNotFound indicates that the requested document is absent.
	ErrDocumentNotFound = errors.New("document not found in graph")
	// ErrBuildBudget indicates an exhausted build limit. Use errors.Is to test it.
	ErrBuildBudget = errors.New("build budget exceeded")
	// ErrQueryBudget indicates an exhausted query limit.
	ErrQueryBudget = graphstore.ErrBudget
	// ErrReadOnly indicates a query that attempts a disallowed operation.
	ErrReadOnly = graphstore.ErrReadOnly
)

// BuildBudgetError describes an addition rejected during graph publication.
// It matches ErrBuildBudget via errors.Is; errors.As exposes the budget details.
// Other build stages may wrap ErrBuildBudget without these details.
// +spec=Structured publication budget errors preserve the ErrBuildBudget category through wrapping.
type BuildBudgetError struct {
	// Stage identifies the publication phase, such as source-use or directory.
	Stage string
	// Resource names the exhausted Options field: MaxNodes, MaxRelations or MaxEvidence.
	Resource string
	// Used is the quantity before the rejected addition; Adding is its size.
	Used, Adding int
	// Limit is the configured maximum for Resource.
	Limit int
}

func (e *BuildBudgetError) Error() string {
	return fmt.Sprintf("%s: %s %s (used=%d, adding=%d, limit=%d)", ErrBuildBudget, e.Stage, e.Resource, e.Used, e.Adding, e.Limit)
}

func (e *BuildBudgetError) Is(target error) bool {
	return target == ErrBuildBudget
}
