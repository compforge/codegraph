package extract

import (
	"sort"

	"github.com/compforge/codegraph/internal/graphmodel"
	"github.com/compforge/codegraph/internal/language"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Capabilities describes implemented extraction/resolution, not grammar availability.
// With no names it returns the language-specific adapters. Pass grammar names
// from Languages to inspect additional outline support without eagerly loading
// every registered grammar.
func Capabilities(languages ...string) []Capability {
	if len(languages) == 0 {
		languages = language.Registered()
	}
	var out []Capability
	for _, name := range languages {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil {
			continue
		}
		c := language.Lookup(entry.Name).Describe(*entry)
		cap := Capability{Language: c.Language, Limitations: c.Limitations}
		for _, v := range c.Organizations {
			cap.Organizations = append(cap.Organizations, graphmodel.NodeKind(v))
		}
		for _, v := range c.Declarations {
			cap.Declarations = append(cap.Declarations, graphmodel.NodeKind(v))
		}
		for _, v := range c.Relations {
			cap.Relations = append(cap.Relations, graphmodel.RelationKind(v))
		}
		for _, v := range c.Markers {
			cap.Markers = append(cap.Markers, graphmodel.MarkerKind(v))
		}
		out = append(out, cap)
	}
	return out
}

// Languages lists registered grammar names without loading their parsers.
// Availability is not a guarantee of extraction or semantic completeness.
func Languages() []string {
	entries := grammars.AllLanguages()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// Language returns the registered grammar name selected for a source path, or
// an empty string. Recognition does not imply complete semantic coverage.
func Language(name string) string {
	if entry := language.Detect(name); entry != nil {
		return entry.Name
	}
	return ""
}

type Capability struct {
	// Organizations lists language units assembled from source contributions.
	Organizations []graphmodel.NodeKind
	Language      string
	Declarations  []graphmodel.NodeKind
	Relations     []graphmodel.RelationKind
	Markers       []graphmodel.MarkerKind
	Limitations   []string
}
