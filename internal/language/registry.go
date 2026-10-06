// Package language assembles immutable built-in adapters. Analysis contracts never import this registry.
package language

import (
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/adapters/ecmascript"
	"github.com/compforge/codegraph/internal/language/adapters/generic"
	"github.com/compforge/codegraph/internal/language/adapters/golang"
	"github.com/compforge/codegraph/internal/language/adapters/manifest"
	"github.com/compforge/codegraph/internal/language/adapters/python"
	"github.com/odvcencio/gotreesitter/grammars"
)

type registration struct {
	name     string
	suffixes []string
	adapter  analysis.Adapter
}

// The registry is built once; mutable analysis state belongs to a Build session.
var registrations = []registration{
	{"go", nil, analysis.Adapter{Extractor: golang.Adapter{}, Organizer: golang.Adapter{}, Binder: golang.Adapter{}, Describe: golang.Describe}},
	{"python", []string{".pyi"}, analysis.Adapter{Extractor: python.Adapter{}, Organizer: python.Adapter{}, Binder: python.Adapter{}, Describe: python.Describe}},
	{"javascript", []string{".mjs", ".cjs", ".jsx"}, ecmascriptAdapter()},
	{"typescript", []string{".mts", ".cts"}, ecmascriptAdapter()},
	{"tsx", nil, ecmascriptAdapter()},
	{"gomod", nil, analysis.Adapter{Extractor: manifest.Adapter{}, Organizer: manifest.Adapter{}, Describe: manifest.Describe}},
	{"toml", nil, analysis.Adapter{Extractor: generic.Adapter{}, Describe: generic.Describe}},
	{"json", nil, analysis.Adapter{Extractor: generic.Adapter{}, Describe: generic.Describe}},
}

// Resolution interprets supplied language metadata before cross-document phases.
func Resolution(scope analysis.BuildScope) (analysis.ResolutionContext, []analysis.Gap) {
	return golang.ManifestResolution(scope)
}

func ecmascriptAdapter() analysis.Adapter {
	return analysis.Adapter{Extractor: ecmascript.Adapter{}, Organizer: ecmascript.Adapter{}, Binder: ecmascript.Adapter{}, Describe: ecmascript.Describe}
}
func Lookup(name string) analysis.Adapter {
	for _, r := range registrations {
		if r.name == name {
			return r.adapter
		}
	}
	return analysis.Adapter{Extractor: generic.Adapter{}, Describe: generic.Describe}
}
func Registered() []string {
	var names []string
	for _, r := range registrations {
		names = append(names, r.name)
	}
	return names
}

// Detect keeps grammar availability open to upstream registrations. Suffixes only select known dialect variants.
func Detect(name string) *grammars.LangEntry {
	switch manifest.Format(name) {
	case "gomod":
		return grammars.DetectLanguageByName("gomod")
	case "pyproject":
		return grammars.DetectLanguageByName("toml")
	case "package_json":
		return grammars.DetectLanguageByName("json")
	}
	ext := strings.ToLower(path.Ext(name))
	for _, r := range registrations {
		for _, suffix := range r.suffixes {
			if ext == suffix {
				return grammars.DetectLanguageByName(r.name)
			}
		}
	}
	return grammars.DetectLanguage(name)
}
