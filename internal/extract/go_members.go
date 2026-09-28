package extract

import (
	"go/ast"
	"go/token"
)

// Preserve lexical objects through the whole expression walk. Cyclic assignment
// evidence (x = x.Child) must terminate without inventing an unbounded type tree.
func goMemberReceiverTypes(expr ast.Expr, assignments map[*ast.Object][]ast.Expr, describe func(ast.Expr) *GoType) []*GoType {
	seen := map[*ast.Object]bool{}
	var value func(ast.Expr) []*GoType
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
					out = append(out, value(typ.Expr)...)
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
		case *ast.CallExpr:
			if id, ok := e.Fun.(*ast.Ident); ok && id.Obj == nil && len(e.Args) > 0 {
				switch id.Name {
				case "new", "make":
					return []*GoType{describe(e.Args[0])}
				case "append":
					return value(e.Args[0])
				}
			}
		}
		return nil
	}
	return value(expr)
}
