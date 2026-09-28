package extract

import "go/ast"

// Reuse the object/assignment walk used by call candidates. The descriptor keeps
// actual lexical type declarations, so a local type cannot bind to a namesake.
func goMemberReceiverTypes(expr ast.Expr, assignments map[*ast.Object][]ast.Expr, describe func(ast.Expr) *GoType) []*GoType {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return goMemberReceiverTypes(e.X, assignments, describe)
	case *ast.StarExpr:
		return goMemberReceiverTypes(e.X, assignments, describe)
	case *ast.UnaryExpr:
		return goMemberReceiverTypes(e.X, assignments, describe)
	case *ast.Ident:
		var out []*GoType
		for _, typ := range goObjectTypeExpressions(e, assignments, map[*ast.Object]bool{}) {
			out = append(out, goReceiverType(typ, describe))
		}
		return out
	default:
		return []*GoType{goReceiverType(expr, describe)}
	}
}

func goReceiverType(expr ast.Expr, describe func(ast.Expr) *GoType) *GoType {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return goReceiverType(e.X, describe)
	case *ast.ParenExpr:
		return goReceiverType(e.X, describe)
	case *ast.UnaryExpr:
		return goReceiverType(e.X, describe)
	case *ast.CompositeLit:
		return describe(e.Type)
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "new" && id.Obj == nil && len(e.Args) == 1 {
			return describe(e.Args[0])
		}
		return describe(nil)
	default:
		return describe(expr)
	}
}
