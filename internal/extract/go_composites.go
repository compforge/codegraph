package extract

import (
	"go/ast"
	"go/token"
)

// Composite literal keys are contextual: the parser's Obj on a field key can
// point at an unrelated lexical variable. Keep that binding only as the map or
// array alternative, and let the loaded type decide which interpretation applies.
func goCompositeKeys(f *Facts, file *ast.File, fset *token.FileSet) map[*ast.Ident]*GoCompositeType {
	declarations := map[int]int{}
	for i, d := range f.Declarations {
		declarations[d.Start] = i
	}
	describe := func(expr ast.Expr) *GoCompositeType {
		hint := &GoCompositeType{Target: -1}
	unwrap:
		for {
			switch n := expr.(type) {
			case *ast.ParenExpr:
				expr = n.X
			case *ast.IndexExpr:
				expr = n.X
			case *ast.IndexListExpr:
				expr = n.X
			default:
				break unwrap
			}
		}
		switch n := expr.(type) {
		case *ast.StructType:
			hint.Kind = "struct"
		case *ast.MapType:
			hint.Kind = "map"
		case *ast.ArrayType:
			hint.Kind = "array"
		case *ast.Ident:
			hint.Name = n.Name
			if n.Obj != nil {
				hint.Bound = true
				if decl, ok := n.Obj.Decl.(*ast.TypeSpec); ok {
					if i, found := declarations[fset.Position(decl.Pos()).Offset]; found {
						hint.Target = i
					}
				}
			}
		case *ast.SelectorExpr:
			if qualifier, ok := n.X.(*ast.Ident); ok && qualifier.Obj == nil {
				hint.Name, hint.Module = n.Sel.Name, qualifier.Name
			}
		}
		return hint
	}
	keys := map[*ast.Ident]*GoCompositeType{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.TypeSpec:
			if i, ok := declarations[fset.Position(n.Pos()).Offset]; ok {
				f.Declarations[i].GoType = describe(n.Type)
			}
		case *ast.CompositeLit:
			hint := describe(n.Type)
			for _, element := range n.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok {
						keys[key] = hint
					}
				}
			}
		}
		return true
	})
	// Known index keys have ordinary expression semantics, including predeclared
	// values such as nil/true. Leave them on the existing lexical extraction path.
	for key, hint := range keys {
		if goIndexKey(f, hint, map[int]bool{}) {
			delete(keys, key)
		}
	}
	return keys
}

func goIndexKey(f *Facts, hint *GoCompositeType, seen map[int]bool) bool {
	if hint == nil {
		return false
	}
	if hint.Kind == "map" || hint.Kind == "array" {
		return true
	}
	if !hint.Bound || hint.Target < 0 || seen[hint.Target] {
		return false
	}
	seen[hint.Target] = true
	return goIndexKey(f, f.Declarations[hint.Target].GoType, seen)
}
