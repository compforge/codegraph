// Evidence precision is part of the public value model.
package pkg

// Confidence describes static evidence precision, not a calibrated probability.
type Confidence string

const (
	Exact     Confidence = "exact"
	Scoped    Confidence = "scoped"
	NameOnly  Confidence = "name_only"
	Heuristic Confidence = "heuristic"
)

func (c Confidence) rank() int {
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
func (c Confidence) Valid() bool { return c.rank() != 0 }

// AtLeast compares evidence precision; unknown values never satisfy a threshold.
func (c Confidence) AtLeast(other Confidence) bool {
	return c.Valid() && other.Valid() && c.rank() >= other.rank()
}

// Stronger combines independent proofs without promoting repeated weak evidence.
func (c Confidence) Stronger(other Confidence) Confidence {
	if other.rank() > c.rank() {
		return other
	}
	return c
}

// Weaker caps a derivation at its weakest necessary premise.
func (c Confidence) Weaker(other Confidence) Confidence {
	if other.rank() < c.rank() {
		return other
	}
	return c
}
