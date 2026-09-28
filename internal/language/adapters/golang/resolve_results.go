package golang

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func goCallResultTypes(ctx context.Context, f analysis.Facts, result *GoType, files map[string]analysis.Facts, module string, methods *methodIndex, limit int) ([]goReceiverValue, error) {
	var out []goReceiverValue
	seen := map[Ref]bool{}
	var signature func(analysis.Facts, *GoType) error
	declaration := func(target Ref) error {
		if seen[target] {
			return nil
		}
		seen[target] = true
		owner := files[target.Path]
		return signature(owner, typeShape(owner.Declarations[target.Declaration].Extension))
	}
	signature = func(source analysis.Facts, hint *GoType) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hint == nil {
			return nil
		}
		switch hint.Kind {
		case "function":
			if result.Result < len(hint.Results) {
				out = append(out, goReceiverValue{source, hint.Results[result.Result]})
			}
			return nil
		case "function_ref":
			if hint.Bound {
				if hint.Target >= 0 {
					return declaration(analysis.SourceRef(source.Path, hint.Target))
				}
				return nil
			}
			edges, err := resolveCallTargets(ctx, source, analysis.Call{Targets: []analysis.CallTarget{{Kind: "function", Name: hint.Name, Module: hint.Module}}}, files, module, methods, limit)
			if err != nil {
				return err
			}
			for _, edge := range edges {
				if err := declaration(edge.Target); err != nil {
					return err
				}
			}
			return nil
		case "member":
			members, err := goMemberReferenceTargets(ctx, source, analysis.Reference{Name: hint.Name, Extension: usageHints{Receivers: hint.Inputs}}, files, module, methods, limit)
			if err != nil {
				return err
			}
			for _, member := range members {
				if err := declaration(member); err != nil {
					return err
				}
			}
			return nil
		}
		values, err := goReceiverValues(ctx, source, hint, files, module, methods, limit)
		if err != nil {
			return err
		}
		for _, value := range values {
			if value.hint.Kind == "function" || value.hint.Kind == "function_ref" {
				if err := signature(value.source, value.hint); err != nil {
					return err
				}
				continue
			}
			typ := value.hint
			if typ.Bound {
				if typ.Target >= 0 {
					if err := declaration(analysis.SourceRef(value.source.Path, typ.Target)); err != nil {
						return err
					}
				}
			} else if typ.Name != "" {
				for _, target := range goTypeTargets(value.source, typ.Name, typ.Module, files, methods.namespaces, module) {
					if err := declaration(target.Ref); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, hint := range result.Inputs {
		if err := signature(f, hint); err != nil {
			return nil, err
		}
	}
	return out, nil
}
