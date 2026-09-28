package golang

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func (Adapter) Bind(ctx context.Context, scope analysis.BuildScope, index *analysis.Index, limit int) (analysis.BindResult, error) {
	edges, issues, err := bind(ctx, scope, index, limit)
	if err != nil {
		return analysis.BindResult{}, err
	}
	return analysis.BindResult{Edges: edges, Issues: issues, Resolver: session{scope}}, nil
}
func bind(ctx context.Context, scope analysis.BuildScope, index *analysis.Index, limit int) ([]Edge, []Gap, error) {
	files, names, module := scope.Files, scope.Names, scope.Module
	namespaces := newNamespaces(index)
	edges, issues, err := resolveTypeRelations(ctx, files, names, module, namespaces, limit)
	if err != nil {
		return nil, nil, err
	}
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := files[name]
		for i, d := range f.Declarations {
			if d.Receiver == "" {
				continue
			}
			var targets []Ref
			for _, target := range namespaces.Namespace(namespaces.Roots[name]).Members(d.Receiver) {
				if !target.IsDeclaration() {
					continue
				}
				switch files[target.Path].Declarations[target.Declaration].Kind {
				case "struct", "interface", "type", "type_alias":
				default:
					continue
				}
				if strings.HasSuffix(target.Path, "_test.go") && !strings.HasSuffix(name, "_test.go") {
					continue
				}
				targets = append(targets, target)
			}
			confidence := "exact"
			if len(targets) == 0 {
				issues = append(issues, Gap{Path: name, Code: "unresolved_receiver", Reference: d.Receiver, Relation: "contains", Span: d.Span})
			} else if len(targets) > 1 {
				confidence = "candidate"
			}
			for _, owner := range targets {
				if err := add(Edge{Source: owner, Target: analysis.SourceRef(name, i), Kind: "contains", Confidence: confidence, Basis: "receiver_declaration", Path: name, Span: d.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
		for _, imp := range f.Imports {
			targets := namespaces.GoImportFiles(module, imp.Path)
			var gitlinks []string
			if dir, ok := ImportDir(module, imp.Path); ok {
				gitlinks = index.GitlinkPaths([]string{dir})
			}
			for _, target := range gitlinks {
				if err := add(Edge{Source: analysis.DocumentRef(name), Target: analysis.DocumentRef(target), Kind: "imports", Confidence: "exact", Basis: "gitlink_boundary", Path: name, Span: imp.Span}); err != nil {
					return nil, nil, err
				}
			}
			if len(targets)+len(gitlinks) == 0 {
				issues = append(issues, Gap{Path: name, Code: "unresolved_import", Reference: imp.Path, Relation: "imports", Span: imp.Span})
			}
			confidence := "exact"
			if namespaces.PackageCount(targets) > 1 {
				confidence = "candidate"
			}
			seenPackages := map[analysis.Ref]bool{}
			for _, target := range targets {
				key := namespaces.Roots[target]
				if !seenPackages[key] {
					if err := add(Edge{Source: analysis.SourceRef(name, -1), Target: key, Kind: "imports", Confidence: confidence, Basis: "package_import", Path: name, Span: imp.Span}); err != nil {
						return nil, nil, err
					}
					seenPackages[key] = true
				}

			}
		}

	}
	return edges, issues, nil
}
