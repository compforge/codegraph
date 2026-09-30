package codegraph

import (
	"github.com/compforge/codegraph/internal/graphstore"
	"github.com/compforge/codegraph/internal/model"
)

// Error sentinels preserve errors.Is across extraction, build and query boundaries.
var (
	ErrSnapshotChanged  = model.ErrSnapshotChanged
	ErrDocumentNotFound = model.ErrDocumentNotFound
	ErrBuildBudget      = model.ErrBuildBudget
	ErrQueryBudget      = graphstore.ErrBudget
	ErrReadOnly         = graphstore.ErrReadOnly
)
