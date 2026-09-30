package codegraph

import (
	"github.com/compforge/codegraph/internal/extract"
)

// Capability describes supported extraction and resolution rather than grammar availability.
type Capability = extract.Capability

// Capabilities describes implemented extraction/resolution, not grammar availability.
// With no names it returns the language-specific adapters. Pass grammar names
// from Languages to inspect additional outline support without eagerly loading
// every registered grammar.
func Capabilities(languages ...string) []Capability { return extract.Capabilities(languages...) }

// Languages lists registered grammar names without loading their parsers.
// Availability is not a guarantee of extraction or semantic completeness.
func Languages() []string { return extract.Languages() }

// Language returns the registered grammar name selected for a source path, or
// an empty string. Recognition does not imply complete semantic coverage.
func Language(name string) string { return extract.Language(name) }
