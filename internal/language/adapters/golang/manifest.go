package golang

import (
	"encoding/json"
	"path"

	"github.com/compforge/codegraph/internal/analysis"
)

func ModuleEntity(root, name string) analysis.Entity {
	data, _ := json.Marshal([]string{"go", "Module", root, name})
	return analysis.Entity{Ref: analysis.SyntheticRef(string(data)), Kind: "Module", Name: name, QualifiedName: name, Language: "go"}
}

// ManifestResolution applies supplied source declarations before organization
// and binding. It copies context so another build or old Graph cannot be changed.
// +spec=`A supplied go.mod declaration wins over conflicting caller hints, with a diagnostic`
func ManifestResolution(scope analysis.BuildScope) (analysis.ResolutionContext, []analysis.Gap) {
	resolution := scope.Resolution.WithGoModule(scope.Module)
	modules := make(map[string]string, len(resolution.GoModules))
	for root, name := range resolution.GoModules {
		modules[root] = name
	}
	var gaps []analysis.Gap
	for _, p := range scope.Names {
		f := scope.Files[p]
		if f.Manifest == nil || f.Manifest.Format != "gomod" || f.Manifest.Name == "" {
			continue
		}
		root, name := path.Dir(p), f.Manifest.Name
		if old := modules[root]; old != "" && old != name {
			gaps = append(gaps, analysis.Gap{Path: p, Span: f.Manifest.NameSpan, Code: "conflicting_module_context", Reference: "supplied go.mod overrides conflicting module context", Relation: "contains"})
		}
		modules[root] = name
	}
	resolution.GoModules = modules
	return resolution, gaps
}
