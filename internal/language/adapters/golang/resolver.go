package golang

import (
	"context"
	"path"

	"github.com/compforge/codegraph/internal/analysis"
)

type session struct{ scope analysis.BuildScope }

func (s session) Resolve(ctx context.Context, index *analysis.Index, limit int) ([]Edge, []Gap, error) {
	files, names, module := s.scope.Files, s.scope.Names, s.scope.Module
	methods := &methodIndex{newNamespaces(index), names, analysis.NewMethodIndex(index)}
	namespaces := methods.namespaces
	var edges []Edge
	var issues []Gap
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
		imports := importedFiles(f, namespaces, module)
		for _, call := range f.Calls {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			source := analysis.SourceRef(name, -1)
			best := len(f.Source) + 1
			for i, d := range f.Declarations {
				if d.Start <= call.Start && d.End >= call.End && d.End-d.Start < best {
					source = analysis.DeclarationRef(name, i)
					best = d.End - d.Start
				}
			}
			refname := call.Name
			if call.Receiver != "" {
				refname = call.Receiver + "." + call.Name
			}
			supplement, err := resolveCallTargets(ctx, f, call, files, module, methods, limit-len(edges))
			if err != nil {
				return nil, nil, err
			}
			if len(supplement) > 0 {
				edges = append(edges, supplement...)
				continue
			}
			if call.Blocked {
				issues = append(issues, Gap{Path: name, Code: "dynamic_call", Reference: refname, Relation: "calls", Span: call.Span})
				continue
			}
			var candidates []Ref
			candidateFiles := imports[call.Receiver]
			basis := "imported_function"
			if call.Receiver == "" {
				basis = "package_function"
				candidateFiles = namespaces.ScopeFiles(f)
				candidateFiles = append(candidateFiles, imports["."]...)
			}
			seen := map[Ref]bool{}
			for _, n := range candidateFiles {
				for i, d := range files[n].Declarations {
					if d.Name != call.Name || d.Kind != "function" {
						continue
					}
					if n != name && path.Dir(n) != path.Dir(name) && !exported(d.Name) {
						continue
					}
					r := analysis.SourceRef(n, i)
					if !seen[r] {
						candidates = append(candidates, r)
						seen[r] = true
					}
				}
			}
			if len(candidates) == 0 {
				if !call.Builtin {
					issues = append(issues, Gap{Path: name, Code: "unresolved_call", Reference: refname, Relation: "calls", Span: call.Span})
				}
				continue
			}
			confidence := analysis.Exact
			if len(candidates) > 1 {
				confidence = "scoped"
			}
			for _, target := range candidates {
				if err := add(Edge{Source: source, Target: target, Kind: "calls", Confidence: confidence, Basis: basis, Path: name, Span: call.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	dispatch, err := resolveInterfaceCalls(ctx, files, edges, module, methods, limit-len(edges))
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, dispatch...)
	references, gaps, err := resolveReferences(ctx, files, names, module, methods, limit-len(edges))
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, references...)
	issues = append(issues, gaps...)
	return edges, issues, nil
}
