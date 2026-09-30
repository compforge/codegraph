package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

type moduleBinder struct {
	ctx        context.Context
	files      map[string]analysis.Facts
	limit      int
	namespaces *NamespaceIndex
}
type exportKey struct{ Path, Name string }

func (b moduleBinder) importTargets(f analysis.Facts, imp analysis.Import, name string, seen map[exportKey]bool) ([]bindingTarget, error) {
	paths := b.namespaces.ModulePaths(f, imp)
	var out []bindingTarget
	for _, p := range paths {
		targets, err := b.exportTargets(p, name, seen)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			target.Confidence = target.Confidence.Weaker(b.namespaces.ImportConfidence(f, imp, len(paths)))
			out = append(out, target)
			if len(out) > b.limit {
				return nil, ErrEdgeLimit
			}
		}
	}
	return uniqueBindingTargets(out), nil
}

// +why=`Export aliases must resolve through the supplied module chain rather than same-name repository search`
func (b moduleBinder) exportTargets(p, name string, seen map[exportKey]bool) ([]bindingTarget, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	key := exportKey{p, name}
	if seen[key] {
		return nil, nil
	}
	seen[key] = true
	defer delete(seen, key)
	f := b.files[p]
	local, explicit := b.namespaces.policy.ExportName(f, name)
	var out []bindingTarget
	direct := explicit
	for _, imp := range f.Imports {
		for _, binding := range imp.Bindings {
			if binding.ReExport && !binding.Namespace && binding.Local == name && binding.Name != "*" {
				direct = true
			}
		}
	}
	if explicit {
		for _, ref := range b.namespaces.Namespace(b.namespaces.Roots[p]).Members(local) {
			out = append(out, bindingTarget{Ref: ref, Confidence: "exact"})
		}
	}

	for _, imp := range f.Imports {
		for _, binding := range imp.Bindings {
			wanted := ""
			if binding.ReExport && !binding.Namespace && binding.Local == name {
				wanted = binding.Name
			}
			if binding.ReExport && !binding.Namespace && binding.Name == "*" && name != "default" && !direct {
				wanted = name
			}
			// A local export can forward a previously imported binding. Python
			// module-level imports are also accessible through the module namespace.
			if explicit && !binding.ReExport && !binding.Namespace && binding.Local == local && enclosingDeclaration(f, binding.Span) == -1 {
				wanted = binding.Name
			}
			if wanted == "" {
				continue
			}
			targets, err := b.importTargets(f, imp, wanted, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, targets...)
			if len(out) > b.limit {
				return nil, ErrEdgeLimit
			}
		}
	}
	return uniqueBindingTargets(out), nil
}

func (b moduleBinder) useTargets(f analysis.Facts, name, receiver string, span analysis.Span) ([]bindingTarget, analysis.MatchState, error) {
	local := name
	if receiver != "" {
		local = receiver
	}
	var out []bindingTarget
	matched := false
	for _, imp := range f.Imports {
		for _, binding := range imp.Bindings {
			if binding.ReExport || binding.Local != local || binding.Namespace != (receiver != "") && receiver != "" {
				continue
			}
			owner := enclosingDeclaration(f, binding.Span)
			if owner >= 0 && (f.Declarations[owner].Start > span.Start || f.Declarations[owner].End < span.End) {
				continue
			}
			matched = true
			if binding.Namespace && receiver == "" {
				paths := b.namespaces.ModulePaths(f, imp)
				var modules []bindingTarget
				for _, p := range paths {
					if target, ok := b.files[p]; ok && b.namespaces.policy.Compatible(target.Language) {
						modules = append(modules, bindingTarget{Ref: b.namespaces.Roots[p], Confidence: "exact"})
					}
				}
				for i := range modules {
					modules[i].Confidence = modules[i].Confidence.Weaker(b.namespaces.ImportConfidence(f, imp, len(modules)))
				}
				out = append(out, modules...)
				continue
			}
			imported := binding.Name
			if binding.Namespace {
				imported = name
			}
			// Unaliased Python dotted imports bind their first segment, not the
			// full module. Do not infer nested runtime attributes from that binding.
			if binding.Namespace && b.namespaces.policy.NestedImport(imp) {
				continue
			}
			targets, err := b.importTargets(f, imp, imported, map[exportKey]bool{})
			if err != nil {
				return nil, analysis.Unresolved, err
			}
			out = append(out, targets...)
			if len(out) > b.limit {
				return nil, analysis.Unresolved, ErrEdgeLimit
			}
		}
	}
	out = uniqueBindingTargets(out)
	if !matched {
		return out, analysis.NotApplicable, nil
	}
	return out, analysis.TargetState(out), nil
}

func (b moduleBinder) importEdges(f analysis.Facts) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	for _, imp := range f.Imports {
		for _, binding := range imp.Bindings {
			if binding.Namespace || binding.Name == "*" {
				continue
			}
			targets, err := b.importTargets(f, imp, binding.Name, map[exportKey]bool{})
			if err != nil {
				return nil, nil, err
			}
			if len(targets) == 0 {
				issues = append(issues, Issue{Path: f.Path, Code: "unresolved_import_binding", Reference: binding.Name, Relation: "imports", Span: binding.Span})
			}
			for _, target := range targets {
				if len(edges) >= b.limit {
					return nil, nil, ErrEdgeLimit
				}
				basis := "named_import"
				if binding.ReExport {
					basis = "re_export"
				}
				edges = append(edges, Edge{Source: analysis.SourceRef(f.Path, enclosingDeclaration(f, binding.Span)), Target: target.Ref, Kind: "imports", Confidence: target.Confidence, Basis: basis, Path: f.Path, Span: binding.Span})
			}
		}
	}
	return edges, issues, nil
}
