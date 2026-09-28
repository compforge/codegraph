package golang

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveCallTargets(ctx context.Context, f analysis.Facts, call analysis.Call, files map[string]analysis.Facts, module string, methods *methodIndex, limit int) ([]Edge, error) {
	var edges []Edge
	source := analysis.SourceRef(f.Path, enclosingDeclaration(f, call.Span))
	// Preserve named-type evidence; projections supplement it only when needed.
	projected := false
	for _, hint := range usage(call.Extension).Receivers {
		switch hint.Kind {
		case "result", "member", "element", "range_key", "range_value":
			projected = true
		}
	}
	if projected {
		targets, err := goMemberReferenceTargets(ctx, f, analysis.Reference{Name: call.Name, Extension: usageHints{Receivers: usage(call.Extension).Receivers}}, files, module, methods, limit)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 {
			for _, target := range targets {
				if files[target.Path].Declarations[target.Declaration].Kind == "method" {
					edges = append(edges, Edge{Source: source, Target: target, Kind: "calls", Confidence: "candidate", Basis: "receiver_type", Path: f.Path, Span: call.Span})
				}
			}
			return edges, nil
		}
	}
	seen := map[Ref]bool{}
	for _, hint := range call.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var targets []Ref
		inherited := map[Ref]bool{}

		dir := path.Dir(f.Path)
		if hint.Module != "" {
			dir = ""
			for _, imp := range f.Imports {
				alias := imp.Alias
				if alias == "" {
					alias = path.Base(imp.Path)
					if imported, ok := ImportDir(module, imp.Path); ok {
						for p, target := range files {
							if path.Dir(p) == imported && target.Language == "go" && target.Package == hint.Module {
								alias = hint.Module
							}
						}
					}
				}
				if alias == hint.Module {
					if imported, ok := ImportDir(module, imp.Path); ok {
						dir = imported
					}
				}
			}
		}
		for name, target := range files {
			if dir == "" || target.Language != "go" || path.Dir(name) != dir || hint.Module == "" && target.Package != f.Package || strings.HasSuffix(name, "_test.go") && !strings.HasSuffix(f.Path, "_test.go") {
				continue
			}
			for i, d := range target.Declarations {
				if d.Name != hint.Name {
					continue
				}
				if hint.Module != "" && !exported(d.Name) {
					continue
				}
				if hint.Kind == "function" && d.Kind == "function" && d.Parent == -1 {
					targets = append(targets, analysis.SourceRef(name, i))
				}
				if hint.Kind == "method" && d.Kind == "method" && (hint.ReceiverType == "" || d.Receiver == hint.ReceiverType || d.Parent >= 0 && target.Declarations[d.Parent].Name == hint.ReceiverType) {
					targets = append(targets, analysis.SourceRef(name, i))
				}
			}
		}

		if hint.Kind == "method" && hint.ReceiverType != "" {
			var roots []Ref
			for _, target := range goTypeTargets(f, hint.ReceiverType, hint.Module, files, methods.namespaces, module) {
				roots = append(roots, target.Ref)
			}
			found, err := methods.lookup(ctx, roots, hint.Name, strings.HasSuffix(f.Path, "_test.go"), limit-len(edges))
			if err != nil {
				return nil, err
			}
			for _, target := range found {
				if hint.Module != "" && !exported(hint.Name) {
					continue
				}
				targets = append(targets, target.Ref)
				inherited[target.Ref] = target.Inherited
			}
		}
		for _, target := range targets {
			if seen[target] {
				continue
			}
			if len(edges) >= limit {
				return nil, ErrEdgeLimit
			}
			seen[target] = true
			basis := hint.Basis
			if inherited[target] {
				basis = "inherited_method"
			}
			edges = append(edges, Edge{Source: source, Target: target, Kind: "calls", Confidence: "candidate", Basis: basis, Path: f.Path, Span: call.Span})
		}
	}
	return edges, nil
}
