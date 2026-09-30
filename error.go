package codegraph

import (
	"github.com/compforge/codegraph/internal/graphmodel"
	"github.com/compforge/codegraph/internal/graphstore"
)

// Error sentinels preserve errors.Is across extraction, build and query boundaries.
var (
	ErrSnapshotChanged  = graphmodel.ErrSnapshotChanged
	ErrDocumentNotFound = graphmodel.ErrDocumentNotFound
	ErrBuildBudget      = graphmodel.ErrBuildBudget
	ErrQueryBudget      = graphstore.ErrBudget
	ErrReadOnly         = graphstore.ErrReadOnly
)
