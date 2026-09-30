package extract

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

// Material exposes immutable extraction material only to sibling internal packages.
// Public aliases intentionally expose no raw-material accessor method.
func Material(f Facts) (analysis.Facts, bool) {
	if f.raw == nil {
		return analysis.Facts{}, false
	}
	return *f.raw, true
}
func Project(f analysis.Facts) (Facts, error) { return projectFacts(f) }
func ExtractMaterial(ctx context.Context, e *Extractor, d Document) (analysis.Facts, error) {
	return e.extract(ctx, d)
}
func Validate(d Document) error                      { return d.validate() }
func Same(a, b Document) bool                        { return a.same(b) }
func Matches(d Document, f analysis.Facts) bool      { return d.matches(f) }
func Size(d Document) int64                          { return d.size() }
func Digest(d Document) [32]byte                     { return d.digest() }
func ValidateBoundaries(d map[string]Document) error { return validateDocumentBoundaries(d) }

// ParseObserver is test instrumentation; set only while no unrelated parser runs.
var ParseObserver func(string)
