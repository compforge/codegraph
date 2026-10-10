package golang

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/compforge/codegraph/internal/language/syntax"
)

func extractGoReferences(f *Facts, file *ast.File, fset *token.FileSet, assignments map[*ast.Object][]goAssignment) {
	keys := goCompositeKeys(f, file, fset)
	excluded := map[*ast.Ident]bool{file.Name: true}
	receivers := map[*ast.Ident]string{}
	members := map[*ast.Ident][]*GoType{}
	describe := goTypeDescriber(f, fset)
	selectors := map[*ast.Ident]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ImportSpec:
			if n.Name != nil {
				excluded[n.Name] = true
			}
		case *ast.FuncDecl:
			excluded[n.Name] = true
			if n.Recv != nil {
				ast.Inspect(n.Recv, func(node ast.Node) bool {
					switch x := node.(type) {
					case *ast.IndexExpr:
						if id, ok := x.Index.(*ast.Ident); ok {
							excluded[id] = true
						}
					case *ast.IndexListExpr:
						for _, index := range x.Indices {
							if id, ok := index.(*ast.Ident); ok {
								excluded[id] = true
							}
						}
					}
					return true
				})
			}
		case *ast.Field:
			for _, name := range n.Names {
				excluded[name] = true
			}
		case *ast.SelectorExpr:
			selectors[n.Sel] = true
			if recv, ok := n.X.(*ast.Ident); ok {
				receivers[n.Sel] = recv.Name
				if recv.Obj != nil {
					members[n.Sel] = goMemberReceiverTypes(n.X, assignments, describe)
				}
			} else {
				members[n.Sel] = goMemberReceiverTypes(n.X, assignments, describe)
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || excluded[id] || id.Name == "_" || types.Universe.Lookup(id.Name) != nil && id.Obj == nil && keys[id] == nil && !selectors[id] || id.Obj != nil && id.Obj.Pos() == id.Pos() {
			return true
		}
		span := Span{Start: fset.Position(id.Pos()).Offset, End: fset.Position(id.End()).Offset}
		ref := Reference{Name: id.Name, Receiver: receivers[id], Extension: usageHints{Key: keys[id]}, Span: span, Owner: syntax.ReferenceOwner(f, span), Target: -1}
		if hints, member := members[id]; member {
			ref.Member = true
			ref.Extension = usageHints{Key: keys[id], Receivers: hints}
		}
		if id.Obj != nil && !ref.Member {
			ref.Bound = true
			start := -1
			switch decl := id.Obj.Decl.(type) {
			case *ast.FuncDecl:
				start = fset.Position(decl.Pos()).Offset
			case *ast.TypeSpec:
				start = fset.Position(decl.Pos()).Offset
			case *ast.ValueSpec:
				// A ValueSpec shares syntax across names, but each name has its own
				// declaration node. Follow the AST binding to preserve shadowing.
				for _, name := range decl.Names {
					if name.Obj == id.Obj {
						start = fset.Position(name.Pos()).Offset
						break
					}
				}
			}

			for i, d := range f.Declarations {
				if d.Name == id.Name && d.Start == start {
					ref.Target = i
					break
				}
			}
		}
		f.References = append(f.References, ref)
		return true
	})
}
