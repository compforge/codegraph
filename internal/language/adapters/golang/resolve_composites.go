package golang

import (
	"context"
	"path"

	"github.com/compforge/codegraph/internal/analysis"
)

func goCompositeKeyTargets(ctx context.Context, f analysis.Facts, ref analysis.Reference, files map[string]analysis.Facts, namespaces *NamespaceIndex, module string, limit int) ([]Ref, bool, error) {
	var fields []Ref
	seen := map[Ref]bool{}
	unique := map[Ref]bool{}
	resolved := map[Ref]bool{}
	var visit func(analysis.Facts, *GoType, *Ref) (bool, error)
	visit = func(source analysis.Facts, hint *GoType, owner *Ref) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if hint == nil {
			return false, nil
		}
		switch hint.Kind {
		case "map", "array":
			return true, nil
		case "struct":
			if owner != nil && (source.Package == f.Package && path.Dir(source.Path) == path.Dir(f.Path) || exported(ref.Name)) {
				for _, target := range namespaces.Namespace(*owner).Members(ref.Name) {
					d := files[target.Path].Declarations[target.Declaration]
					if d.Parent == owner.Declaration && d.Kind == "field" && d.Name == ref.Name {
						if !unique[target] {
							if len(fields) >= limit {
								return false, ErrEdgeLimit
							}
							unique[target] = true
							fields = append(fields, target)
						}
					}
				}
			}
			return false, nil
		}
		var targets []Ref
		if hint.Bound {
			if hint.Target >= 0 {
				targets = append(targets, analysis.SourceRef(source.Path, hint.Target))
			}
		} else if hint.Name != "" {
			for _, target := range goTypeTargets(source, hint.Name, hint.Module, files, namespaces, module) {
				targets = append(targets, target.Ref)
			}
		}
		ordinary := len(targets) > 0
		for _, target := range targets {
			if prior, ok := resolved[target]; ok {
				ordinary = ordinary && prior
				continue
			}
			if seen[target] {
				ordinary = false
				continue
			}
			seen[target] = true
			typ := files[target.Path]
			valueKey, err := visit(typ, typeShape(typ.Declarations[target.Declaration].Extension), &target)
			if err != nil {
				return false, err
			}
			resolved[target] = valueKey
			ordinary = ordinary && valueKey
			delete(seen, target)
		}
		return ordinary, nil
	}
	ordinary, err := visit(f, usage(ref.Extension).Key, nil)
	return fields, ordinary, err
}
