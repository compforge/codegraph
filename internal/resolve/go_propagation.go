package resolve

import (
	"context"

	"github.com/compforge/codegraph/internal/extract"
)

type goReceiverValue struct {
	source extract.Facts
	hint   *extract.GoType
}

// Project through loaded field declarations and container shapes only. The
// source travels with each type: imports and lexical indices belong to the
// declaring file, not the caller's file. Function return values remain unknown.
func goReceiverValues(ctx context.Context, source extract.Facts, hint *extract.GoType, files map[string]extract.Facts, module string, methods *methodIndex, limit int) ([]goReceiverValue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hint == nil {
		return nil, nil
	}
	if hint.Kind == "member" {
		members, err := goMemberReferenceTargets(ctx, source, extract.Reference{Name: hint.Name, ReceiverTypes: hint.Inputs}, files, module, methods, limit)
		if err != nil {
			return nil, err
		}
		var out []goReceiverValue
		for _, member := range members {
			owner := files[member.Path]
			d := owner.Declarations[member.Declaration]
			if d.Kind == "field" && d.GoType != nil {
				out = append(out, goReceiverValue{owner, d.GoType})
			}
		}
		return out, nil
	}
	if hint.Kind != "element" && hint.Kind != "range_key" && hint.Kind != "range_value" {
		return []goReceiverValue{{source, hint}}, nil
	}
	var out []goReceiverValue
	seen := map[Ref]bool{}
	var project func(extract.Facts, *extract.GoType) error
	project = func(owner extract.Facts, typ *extract.GoType) error {
		values, err := goReceiverValues(ctx, owner, typ, files, module, methods, limit)
		if err != nil {
			return err
		}
		for _, value := range values {
			shape := value.hint
			var element *extract.GoType
			switch shape.Kind {
			case "map":
				element = shape.Element
				if hint.Kind == "range_key" {
					element = shape.Key
				}
			case "array":
				if hint.Kind != "range_key" {
					element = shape.Element
				}
			case "channel":
				if hint.Kind == "range_key" {
					element = shape.Element
				}
			}
			if element != nil {
				out = append(out, goReceiverValue{value.source, element})
				continue
			}
			var roots []Ref
			if shape.Bound {
				if shape.Target >= 0 {
					roots = append(roots, Ref{value.source.Path, shape.Target})
				}
			} else if shape.Name != "" {
				for _, target := range goTypeTargets(value.source, shape.Name, shape.Module, files, methods.names, module) {
					roots = append(roots, target.Ref)
				}
			}
			for _, root := range roots {
				if seen[root] {
					continue
				}
				seen[root] = true
				owner := files[root.Path]
				if err := project(owner, owner.Declarations[root.Declaration].GoType); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, input := range hint.Inputs {
		if err := project(source, input); err != nil {
			return nil, err
		}
	}
	return out, nil
}
