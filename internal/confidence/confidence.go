// Package confidence defines evidence precision shared by graph producers and API.
package confidence

// Level describes static evidence precision, not a calibrated probability.
type Level string

const (
	Exact     Level = "exact"
	Scoped    Level = "scoped"
	NameOnly  Level = "name_only"
	Heuristic Level = "heuristic"
)

func (c Level) rank() int {
	switch c {
	case Exact:
		return 4
	case Scoped:
		return 3
	case NameOnly:
		return 2
	case Heuristic:
		return 1
	default:
		return 0
	}
}

// Valid reports whether c is one of the four supported evidence tiers.
func (c Level) Valid() bool { return c.rank() != 0 }

// AtLeast compares evidence precision; unknown values never satisfy a threshold.
func (c Level) AtLeast(other Level) bool {
	return c.Valid() && other.Valid() && c.rank() >= other.rank()
}

// Stronger combines independent proofs without promoting repeated weak evidence.
func (c Level) Stronger(other Level) Level {
	if other.rank() > c.rank() {
		return other
	}
	return c
}

// Weaker caps a derivation at its weakest necessary premise.
func (c Level) Weaker(other Level) Level {
	if other.rank() < c.rank() {
		return other
	}
	return c
}
