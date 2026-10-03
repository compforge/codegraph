package module

import (
	"context"
	"github.com/compforge/codegraph/internal/analysis"
)

// sourceItemEdges uses the same namespace and export policy as terminal binding,
// retaining the intermediate source names instead of flattening them away.
func sourceItemEdges(ctx context.Context, f analysis.Facts, namespaces *NamespaceIndex, limit int) ([]Edge, error) {
	b := moduleBinder{ctx, namespaces.Files, limit, namespaces}
	var edges []Edge
	add := func(item analysis.ModuleItem, target analysis.Ref, kind string, c analysis.Confidence, basis string) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, Edge{Source: item.Ref, Target: target, Kind: kind, Confidence: c, Basis: basis, Path: f.Path, Span: item.Span})
		return nil
	}
	for _, item := range analysis.ModuleItems(f) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Import < 0 {
			targets := []bindingTarget{}
			// Exporting an imported local name preserves that binding in the chain.
			if ref, ok := analysis.LocalImport(f, item.Binding.LocalName, item.Span); ok {
				targets = append(targets, bindingTarget{Ref: ref, Confidence: analysis.Exact})
			} else {
				for i, d := range f.Declarations {
					if d.Name == item.Binding.LocalName && d.Parent == item.Owner {
						targets = append(targets, bindingTarget{Ref: analysis.DeclarationRef(f.Path, i), Confidence: analysis.Exact})
					}
				}
			}
			for _, target := range uniqueBindingTargets(targets) {
				if err := add(item, target.Ref, "aliases", target.Confidence, "local_export"); err != nil {
					return nil, err
				}
			}
			continue
		}
		imp := f.Imports[item.Import]
		paths := namespaces.ModulePaths(f, imp)
		c := namespaces.ImportConfidence(f, imp, len(paths))
		for _, p := range paths {
			if err := add(item, namespaces.Roots[p], "imports", c, namespaces.ImportBasis(f, imp)); err != nil {
				return nil, err
			}
		}
		for _, p := range namespaces.GitlinkPaths(namespaces.ImportPaths(f, imp)) {
			if err := add(item, analysis.DocumentRef(p), "imports", c, "gitlink_boundary"); err != nil {
				return nil, err
			}
		}
		if item.Binding.Form == "namespace" {
			// An unaliased Python a.b import binds a, not the loaded a.b module.
			if namespaces.policy.NestedImport(imp) {
				continue
			}
			for _, p := range paths {
				if err := add(item, namespaces.Roots[p], "aliases", c, "namespace_import"); err != nil {
					return nil, err
				}
			}
			continue
		}
		if item.Binding.Form == "wildcard" || item.Binding.Form == "side_effect" {
			continue
		}
		var targets []bindingTarget
		for _, p := range paths {
			found, err := b.exportItems(p, item.Binding.ImportedName, map[exportKey]bool{})
			if err != nil {
				return nil, err
			}
			targets = append(targets, found...)
		}
		for _, target := range uniqueBindingTargets(targets) {
			if err := add(item, target.Ref, "aliases", target.Confidence.Weaker(c), "imported_export"); err != nil {
				return nil, err
			}
		}
	}
	return edges, nil
}

// exportItems stops at an explicit public binding. Wildcard rules select public
// names from their source modules; they are not aliases for the module itself.
func (b moduleBinder) exportItems(p, name string, seen map[exportKey]bool) ([]bindingTarget, error) {
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
	var out []bindingTarget
	for _, item := range analysis.ModuleItems(f) {
		if item.Kind == "Export" && item.Owner < 0 && item.Binding.ExportedName == name {
			out = append(out, bindingTarget{Ref: item.Ref, Confidence: analysis.Exact})
		}
	}
	if len(out) > 0 {
		return uniqueBindingTargets(out), nil
	}
	// Languages without explicit export syntax still expose language-defined names.
	local, visible := b.namespaces.policy.ExportName(f, name)
	if visible {
		for _, ref := range b.namespaces.Namespace(b.namespaces.Roots[p]).Members(local) {
			out = append(out, bindingTarget{Ref: ref, Confidence: analysis.Exact})
		}
		for _, item := range analysis.ModuleItems(f) {
			if item.Kind == "Import" && item.Owner < 0 && item.Binding.LocalName == local {
				out = append(out, bindingTarget{Ref: item.Ref, Confidence: analysis.Exact})
			}
		}
	}
	if len(out) > 0 {
		return uniqueBindingTargets(out), nil
	}
	if name == "default" {
		return nil, nil
	}
	for _, imp := range f.Imports {
		wildcard := false
		for _, binding := range imp.Bindings {
			if binding.ReExport && !binding.Namespace && binding.Name == "*" {
				wildcard = true
			}
		}
		if !wildcard {
			continue
		}
		paths := b.namespaces.ModulePaths(f, imp)
		for _, path := range paths {
			found, err := b.exportItems(path, name, seen)
			if err != nil {
				return nil, err
			}
			for _, target := range found {
				target.Confidence = target.Confidence.Weaker(b.namespaces.ImportConfidence(f, imp, len(paths)))
				out = append(out, target)
				if len(out) > b.limit {
					return nil, ErrEdgeLimit
				}
			}
		}
	}
	return uniqueBindingTargets(out), nil
}
