package confidence

import "testing"

func TestEvidencePrecisionOrder(t *testing.T) {
	levels := []Level{Heuristic, NameOnly, Scoped, Exact}
	for i, a := range levels {
		for j, b := range levels {
			if a.AtLeast(b) != (i >= j) || a.Stronger(b) != levels[max(i, j)] || a.Weaker(b) != levels[min(i, j)] {
				t.Fatalf("inconsistent order: %s %s", a, b)
			}
		}
	}
	for _, invalid := range []Level{"", "candidate", "strong", "other"} {
		if invalid.Valid() || invalid.AtLeast(Heuristic) || Exact.AtLeast(invalid) {
			t.Fatalf("unknown precision accepted: %q", invalid)
		}
	}
}
