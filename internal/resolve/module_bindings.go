package resolve

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

type bindingTarget struct {
	Ref
	Confidence string
}

type moduleBinder struct {
	ctx   context.Context
	files map[string]extract.Facts
	limit int
}

type exportKey struct{ Path, Name string }

func (b moduleBinder) importTargets(f extract.Facts, imp extract.Import, name string, seen map[exportKey]bool) ([]bindingTarget, error) {
	var paths []string
	for _, p := range ImportPaths(f, imp) {
		if target, ok := b.files[p]; ok && compatibleLanguage(f.Language, target.Language) {
			paths = append(paths, p)
		}
	}
	var out []bindingTarget
	for _, p := range paths {
		targets, err := b.exportTargets(p, name, seen)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if len(paths) > 1 || f.Language == "python" && imp.Relative == 0 {
				target.Confidence = "candidate"
			}
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
	local, explicit := f.Exports[name]
	if f.Language == "python" {
		local, explicit = name, true
	}
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
		for i, d := range f.Declarations {
			if d.Parent == -1 && d.Name == local {
				out = append(out, bindingTarget{Ref{p, i}, "exact"})
			}
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

func uniqueBindingTargets(targets []bindingTarget) []bindingTarget {
	out := make([]bindingTarget, 0, len(targets))
	indexes := map[Ref]int{}
	for _, target := range targets {
		if i, ok := indexes[target.Ref]; ok {
			if target.Confidence == "candidate" {
				out[i].Confidence = "candidate"
			}
		} else {
			indexes[target.Ref] = len(out)
			out = append(out, target)
		}
	}
	if len(out) > 1 {
		for i := range out {
			out[i].Confidence = "candidate"
		}
	}
	return out
}

func enclosingDeclaration(f extract.Facts, span extract.Span) int {
	owner, size := -1, len(f.Source)+1
	for i, d := range f.Declarations {
		if d.Start <= span.Start && d.End >= span.End && d.End-d.Start < size {
			owner, size = i, d.End-d.Start
		}
	}
	return owner
}

func (b moduleBinder) useTargets(f extract.Facts, name, receiver string, span extract.Span) ([]bindingTarget, bool, error) {
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
				paths := ImportPaths(f, imp)
				var modules []bindingTarget
				for _, p := range paths {
					if target, ok := b.files[p]; ok && compatibleLanguage(f.Language, target.Language) {
						modules = append(modules, bindingTarget{Ref{p, -1}, "exact"})
					}
				}
				if len(modules) > 1 || f.Language == "python" && imp.Relative == 0 {
					for i := range modules {
						modules[i].Confidence = "candidate"
					}
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
			if binding.Namespace && f.Language == "python" && imp.Alias == "" && strings.Contains(imp.Path, ".") {
				continue
			}
			targets, err := b.importTargets(f, imp, imported, map[exportKey]bool{})
			if err != nil {
				return nil, true, err
			}
			out = append(out, targets...)
			if len(out) > b.limit {
				return nil, true, ErrEdgeLimit
			}
		}
	}
	return uniqueBindingTargets(out), matched, nil
}

func (b moduleBinder) importEdges(f extract.Facts) ([]Edge, []Issue, error) {
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
				issues = append(issues, Issue{f.Path, "unresolved_import_binding", binding.Name, "imports", binding.Span})
			}
			for _, target := range targets {
				if len(edges) >= b.limit {
					return nil, nil, ErrEdgeLimit
				}
				basis := "named_import"
				if binding.ReExport {
					basis = "re_export"
				}
				edges = append(edges, Edge{Ref{f.Path, enclosingDeclaration(f, binding.Span)}, target.Ref, "imports", target.Confidence, basis, f.Path, binding.Span})
			}
		}
	}
	return edges, issues, nil
}
