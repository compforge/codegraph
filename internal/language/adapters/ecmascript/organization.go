package ecmascript

import (
	"context"
	"encoding/json"
	"github.com/compforge/codegraph/internal/analysis"
	"path"
	"strings"
)

// A file module identifies a supplied source unit. It does not assert runtime
// loader mode, global-script sharing, or dependency configuration.
func (Adapter) Organize(ctx context.Context, scope analysis.Scope) (analysis.Organization, error) {
	result := analysis.Organization{Roots: map[string]analysis.Ref{}}
	for _, p := range scope.Names {
		if err := ctx.Err(); err != nil {
			return analysis.Organization{}, err
		}
		f := scope.Files[p]
		name := strings.TrimSuffix(path.Base(p), path.Ext(p))
		key, _ := json.Marshal([]string{f.Language, "Module", p, name})
		ref := analysis.SyntheticRef(string(key))
		result.Entities = append(result.Entities, analysis.Entity{Ref: ref, Kind: "Module", Name: name, QualifiedName: strings.TrimSuffix(p, path.Ext(p)), Language: f.Language})
		result.Roots[p] = ref
		result.Edges = append(result.Edges, analysis.Edge{Source: analysis.DocumentRef(p), Target: ref, Kind: "declares", Confidence: "exact", Basis: "source_namespace", Path: p, Span: analysis.Span{End: len(f.Source)}})
	}
	return result, nil
}
