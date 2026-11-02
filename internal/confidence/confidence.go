package confidence

import "github.com/compforge/codegraph/pkg"

type Level = pkg.Confidence

const (
	Exact     = pkg.Exact
	Scoped    = pkg.Scoped
	NameOnly  = pkg.NameOnly
	Heuristic = pkg.Heuristic
)
