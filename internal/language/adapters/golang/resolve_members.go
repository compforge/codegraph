package golang

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func goMemberReferenceTargets(ctx context.Context, f analysis.Facts, ref analysis.Reference, files map[string]analysis.Facts, module string, methods *methodIndex, limit int) ([]Ref, error) {
	var out []Ref
	unique := map[Ref]bool{}
	add := func(target Ref) error {
		owner := files[target.Path]
		if !exported(ref.Name) && (path.Dir(owner.Path) != path.Dir(f.Path) || owner.Package != f.Package) {
			return nil
		}
		if unique[target] {
			return nil
		}
		if len(out) >= limit {
			return ErrEdgeLimit
		}
		unique[target] = true
		out = append(out, target)
		return nil
	}
	type visit struct {
		target  Ref
		methods bool
	}
	seen := map[visit]bool{}
	var walk func(analysis.Facts, *GoType, bool) error
	walk = func(source analysis.Facts, hint *GoType, withMethods bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hint == nil {
			return nil
		}
		if hint.Kind == "result" || hint.Kind == "member" || hint.Kind == "element" || hint.Kind == "range_key" || hint.Kind == "range_value" {
			values, err := goReceiverValues(ctx, source, hint, files, module, methods, limit)
			if err != nil {
				return err
			}
			for _, value := range values {
				if err := walk(value.source, value.hint, withMethods); err != nil {
					return err
				}
			}
			return nil
		}
		var roots []Ref
		if hint.Bound {
			if hint.Target >= 0 {
				roots = append(roots, analysis.SourceRef(source.Path, hint.Target))
			}
		} else if hint.Name != "" {
			for _, target := range goTypeTargets(source, hint.Name, hint.Module, files, methods.namespaces, module) {
				roots = append(roots, target.Ref)
			}
		}
		for _, root := range roots {
			key := visit{root, withMethods}
			if seen[key] {
				continue
			}
			seen[key] = true
			typ := files[root.Path]
			decl := typ.Declarations[root.Declaration]
			if (path.Dir(typ.Path) != path.Dir(f.Path) || typ.Package != f.Package) && !exported(ref.Name) {
				continue
			}
			for _, member := range methods.namespaces.Members[root][ref.Name] {
				if member.IsDeclaration() && files[member.Path].Declarations[member.Declaration].Kind == "field" {
					if err := add(member); err != nil {
						return err
					}
				}
			}

			// A newly defined interface retains its underlying interface methods;
			// a newly defined concrete type does not inherit receiver methods.
			if withMethods || decl.Kind == "interface" {
				targets, err := methods.lookup(ctx, []Ref{root}, ref.Name, strings.HasSuffix(f.Path, "_test.go"), limit)
				if err != nil {
					return err
				}
				for _, target := range targets {
					if err := add(target.Ref); err != nil {
						return err
					}
				}
			}
			if err := walk(typ, typeShape(decl.Extension), withMethods && decl.Kind == "type_alias"); err != nil {
				return err
			}
		}
		return nil
	}
	for _, hint := range usage(ref.Extension).Receivers {
		if err := walk(f, hint, true); err != nil {
			return nil, err
		}
	}
	return out, nil
}
