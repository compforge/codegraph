package model

import (
	"errors"
)

var (
	ErrSnapshotChanged  = errors.New("source changed within graph snapshot")
	ErrDocumentNotFound = errors.New("document not found in graph")
	ErrBuildBudget      = errors.New("build budget exceeded")
)
