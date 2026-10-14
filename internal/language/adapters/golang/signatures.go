package golang

import (
	"go/ast"

	gts "github.com/odvcencio/gotreesitter"
)

// Signature facts own parameter ordering and grouped-name expansion; this
// projection binds their type syntax to the same AST identities as references.
func applyGoSignatures(f *Facts, signatures []gts.SignatureFact, expressions map[Span]ast.Expr, describe func(ast.Expr) *GoType) {
	byName := map[int][]int{}
	for i, d := range f.Declarations {
		byName[d.NameSpan.Start] = append(byName[d.NameSpan.Start], i)
	}
	for _, s := range signatures {
		shape := &GoType{Kind: "function", Target: -1}
		for _, p := range s.Parameters {
			expr := expressions[Span{Start: int(p.TypeStartByte), End: int(p.TypeEndByte)}]
			var typ *GoType
			if !p.Variadic {
				switch expr.(type) {
				case *ast.Ident, *ast.SelectorExpr:
					typ = describe(expr)
				}
			}
			shape.Parameters = append(shape.Parameters, typ)
		}
		for _, p := range s.Results {
			expr := expressions[Span{Start: int(p.TypeStartByte), End: int(p.TypeEndByte)}]
			shape.Results = append(shape.Results, describe(expr))
		}
		for _, i := range byName[int(s.NameStartByte)] {
			f.Declarations[i].Extension = shape
		}
	}
}
