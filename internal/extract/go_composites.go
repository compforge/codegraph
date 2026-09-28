package extract

import (
	"go/ast"
	"go/token"
)

// Composite literal keys are contextual: the parser's Obj on a field key can
// point at an unrelated lexical variable. Keep that binding only as the map or
// array alternative, and let the loaded type decide which interpretation applies.
func goCompositeKeys(f *Facts, file *ast.File, fset *token.FileSet) map[*ast.Ident]*GoType {
	describe := goTypeDescriber(f, fset)
	declarations := map[int]int{}
	for i, d := range f.Declarations {
		declarations[d.Start] = i
	}
	keys := map[*ast.Ident]*GoType{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if i, ok := declarations[fset.Position(n.Pos()).Offset]; ok {
				f.Declarations[i].GoType = describe(n.Type)
			}
		case *ast.TypeSpec:
			if typ := describe(n.Name); typ.Target >= 0 {
				f.Declarations[typ.Target].GoType = describe(n.Type)
			}
		case *ast.Field:
			for _, name := range n.Names {
				if i, ok := declarations[fset.Position(name.Pos()).Offset]; ok && (f.Declarations[i].Kind == "field" || f.Declarations[i].Kind == "method") {
					f.Declarations[i].GoType = describe(n.Type)
				}
			}
			if len(n.Names) == 0 {
				if i, ok := declarations[fset.Position(n.Pos()).Offset]; ok && (f.Declarations[i].Kind == "field" || f.Declarations[i].Kind == "method") {
					f.Declarations[i].GoType = describe(n.Type)
				}
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

func goIndexKey(f *Facts, hint *GoType, seen map[int]bool) bool {
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

// A single syntax descriptor preserves lexical type identity for keys and receivers.
func goTypeDescriber(f *Facts, fset *token.FileSet) func(ast.Expr) *GoType {
	declarations := map[int]int{}
	for i, d := range f.Declarations {
		declarations[d.Start] = i
	}
	var describe func(ast.Expr) *GoType
	describe = func(expr ast.Expr) *GoType {
		hint := &GoType{Target: -1}
	unwrap:
		for {
			switch n := expr.(type) {
			case *ast.ParenExpr:
				expr = n.X
			case *ast.StarExpr:
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
		case *ast.FuncType:
			hint.Kind = "function"
			if n.Results != nil {
				for _, field := range n.Results.List {
					for i := 0; i < max(1, len(field.Names)); i++ {
						hint.Results = append(hint.Results, describe(field.Type))
					}
				}
			}
		case *ast.StructType:
			hint.Kind = "struct"
		case *ast.MapType:
			hint.Kind = "map"
			hint.Key, hint.Element = describe(n.Key), describe(n.Value)
		case *ast.ArrayType:
			hint.Kind = "array"
			hint.Element = describe(n.Elt)
		case *ast.ChanType:
			hint.Kind = "channel"
			hint.Element = describe(n.Value)
		case *ast.Ident:
			hint.Name = n.Name
			if n.Obj != nil {
				hint.Bound = true
				if fn, ok := n.Obj.Decl.(*ast.FuncDecl); ok {
					hint.Kind = "function_ref"
					if i, found := declarations[fset.Position(fn.Pos()).Offset]; found {
						hint.Target = i
					}
				}
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
	return describe
}
