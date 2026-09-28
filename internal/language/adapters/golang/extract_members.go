package golang

import (
	"go/ast"
	"go/token"
)

func goMemberReceiverTypes(expr ast.Expr, assignments map[*ast.Object][]goAssignment, describe func(ast.Expr) *GoType) []*GoType {
	seen := map[*ast.Object]bool{}
	var value func(ast.Expr) []*GoType
	var result func(*ast.CallExpr, int) []*GoType
	project := func(kind, name string, inputs []*GoType) []*GoType {
		if len(inputs) == 0 {
			return nil
		}
		return []*GoType{{Kind: kind, Name: name, Inputs: inputs, Target: -1}}
	}
	value = func(expr ast.Expr) []*GoType {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			return value(e.X)
		case *ast.StarExpr:
			return value(e.X)
		case *ast.UnaryExpr:
			// Receiving is not a reference to the channel itself.
			if e.Op == token.ARROW {
				return nil
			}
			return value(e.X)
		case *ast.Ident:
			if e.Obj == nil || seen[e.Obj] {
				return nil
			}
			seen[e.Obj] = true
			defer delete(seen, e.Obj)
			var out []*GoType
			for _, typ := range goObjectTypeExpressions(e, assignments, map[*ast.Object]bool{}) {
				switch {
				case typ.IsType:
					out = append(out, describe(typ.Expr))
				case typ.Projection != "":
					out = append(out, project(typ.Projection, "", value(typ.Expr))...)
				default:
					if call, ok := typ.Expr.(*ast.CallExpr); ok {
						out = append(out, result(call, typ.Result)...)
					} else {
						out = append(out, value(typ.Expr)...)
					}
				}
			}
			return out
		case *ast.CompositeLit:
			return []*GoType{describe(e.Type)}
		case *ast.TypeAssertExpr:
			return []*GoType{describe(e.Type)}
		case *ast.SelectorExpr:
			if qualifier, ok := e.X.(*ast.Ident); ok && qualifier.Obj == nil {
				return []*GoType{describe(e)}
			}
			return project("member", e.Sel.Name, value(e.X))
		case *ast.IndexExpr:
			// Qualified/cross-file type applications bind by loaded type identity.
			// A value with that name cannot coexist in the same package scope.
			if selected, ok := e.X.(*ast.SelectorExpr); ok {
				if qualifier, ok := selected.X.(*ast.Ident); ok && qualifier.Obj == nil {
					return []*GoType{describe(e)}
				}
			}
			if id, ok := e.X.(*ast.Ident); ok && (id.Obj == nil || id.Obj.Kind == ast.Typ) {
				return []*GoType{describe(e)}
			}
			return project("element", "", value(e.X))
		case *ast.IndexListExpr:
			return []*GoType{describe(e)}
		case *ast.SliceExpr:
			return value(e.X)
		case *ast.FuncLit:
			return []*GoType{describe(e.Type)}
		case *ast.CallExpr:
			return result(e, 0)
		}
		return nil
	}
	result = func(call *ast.CallExpr, slot int) []*GoType {
		callee := goUnwrapInstantiation(call.Fun)
		if id, ok := callee.(*ast.Ident); ok {
			if id.Obj == nil && slot == 0 && len(call.Args) > 0 {
				switch id.Name {
				case "new", "make":
					return []*GoType{describe(call.Args[0])}
				case "append":
					return value(call.Args[0])
				}
			}
			if id.Obj != nil && id.Obj.Kind == ast.Typ {
				if slot == 0 {
					return []*GoType{describe(id)}
				}
				return nil
			}
		}
		var inputs []*GoType
		switch fn := callee.(type) {
		case *ast.Ident:
			if fn.Obj == nil {
				inputs = []*GoType{{Kind: "function_ref", Name: fn.Name, Target: -1}}
			} else {
				inputs = value(fn)
			}
		case *ast.SelectorExpr:
			if qualifier, ok := fn.X.(*ast.Ident); ok && qualifier.Obj == nil {
				inputs = []*GoType{{Kind: "function_ref", Name: fn.Sel.Name, Module: qualifier.Name, Target: -1}}
			} else {
				inputs = project("member", fn.Sel.Name, value(fn.X))
			}
		default:
			inputs = value(callee)
		}
		if len(inputs) == 0 {
			return nil
		}
		return []*GoType{{Kind: "result", Inputs: inputs, Result: slot, Target: -1}}
	}
	return value(expr)
}
