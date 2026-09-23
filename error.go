package codegraph

import (
	"errors"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Public error sentinels keep errors.Is stable across build and query paths.
var (
	ErrSnapshotChanged  = errors.New("source changed within graph snapshot")
	ErrBuildInProgress  = errors.New("graph flush in progress")
	ErrDocumentNotAdded = errors.New("document was not added to graph")
	ErrBuildBudget      = errors.New("build budget exceeded")
	ErrQueryBudget      = graphstore.ErrBudget
	ErrReadOnly         = graphstore.ErrReadOnly
)
