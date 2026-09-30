package extract

import "testing"

func TestNewExtractionCacheRejectsNegativeLimits(t *testing.T) {
	for _, limits := range []struct {
		documents int
		bytes     int64
	}{{-1, 0}, {0, -1}} {
		if _, err := NewExtractionCache(limits.documents, limits.bytes); err == nil {
			t.Fatal("negative cache limit accepted")
		}
	}
}
