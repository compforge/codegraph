package manifest

import (
	"context"
	"path"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/adapters/golang"
)

func (Adapter) Organize(ctx context.Context, scope analysis.BuildScope) (analysis.Organization, error) {
	out := analysis.Organization{Roots: map[string]analysis.Ref{}}
	for _, p := range scope.Names {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		f := scope.Files[p]
		if f.Manifest == nil || f.Manifest.Format != "gomod" || f.Manifest.Name == "" {
			continue
		}
		owner := golang.ModuleEntity(path.Dir(p), f.Manifest.Name)
		out.Entities = append(out.Entities, owner)
		out.Roots[p] = owner.Ref
		out.Edges = append(out.Edges, analysis.Edge{Source: analysis.DocumentRef(p), Target: owner.Ref, Kind: "declares", Confidence: "exact", Basis: "source_manifest", Path: p, Span: f.Manifest.NameSpan})
	}
	return out, nil
}
