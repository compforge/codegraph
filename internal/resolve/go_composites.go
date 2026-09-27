package resolve

import (
	"context"
	"path"

	"github.com/compforge/codegraph/internal/extract"
)

// Follow only loaded type declarations. Unknown/external types cannot license a
// bare-name fallback: their keys may name fields rather than lexical variables.
func goCompositeKeyTargets(ctx context.Context, f extract.Facts, ref extract.Reference, files map[string]extract.Facts, names []string, module string, limit int) ([]Ref, bool, error) {
	var fields []Ref
	seen := map[Ref]bool{}
	unique := map[Ref]bool{}
	resolved := map[Ref]bool{}
	var visit func(extract.Facts, *extract.GoCompositeType, *Ref) (bool, error)
	visit = func(source extract.Facts, hint *extract.GoCompositeType, owner *Ref) (bool, error) {
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
				for i, d := range source.Declarations {
					if d.Parent == owner.Declaration && d.Kind == "field" && d.Name == ref.Name {
						target := Ref{source.Path, i}
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
				targets = append(targets, Ref{source.Path, hint.Target})
			}
		} else if hint.Name != "" {
			for _, target := range goTypeTargets(source, hint.Name, hint.Module, files, names, module) {
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
			valueKey, err := visit(typ, typ.Declarations[target.Declaration].GoType, &target)
			if err != nil {
				return false, err
			}
			resolved[target] = valueKey
			ordinary = ordinary && valueKey
			delete(seen, target)
		}
		return ordinary, nil
	}
	ordinary, err := visit(f, ref.Key, nil)
	return fields, ordinary, err
}
