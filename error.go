package codegraph

import (
	"errors"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Public error sentinels keep errors.Is stable across build and query paths.
var (
	ErrSnapshotChanged  = errors.New("source changed within graph snapshot")
	ErrDocumentNotFound = errors.New("document not found in graph")
	ErrBuildBudget      = errors.New("build budget exceeded")
	ErrQueryBudget      = graphstore.ErrBudget
	ErrReadOnly         = graphstore.ErrReadOnly
)
