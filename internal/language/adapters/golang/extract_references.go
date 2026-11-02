package golang

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	"github.com/compforge/codegraph/pkg"
)

func extractGoReferences(f *Facts, file *ast.File, fset *token.FileSet, assignments map[*ast.Object][]goAssignment) {
	keys := goCompositeKeys(f, file, fset)
	roles := goUseRoles(file)
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
		ref := Reference{Kind: roles[id], Name: id.Name, Receiver: receivers[id], Extension: usageHints{Key: keys[id]}, Span: span, Owner: syntax.ReferenceOwner(f, span), Target: -1}
		if keys[id] != nil {
			ref.Kind = analysis.CompositeKeyUse
		}
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

// goUseRoles labels only the operated-on expression. In a[i] = v, a and i
// remain reads; in obj.field = v only field is written.
func goUseRoles(file *ast.File) map[*ast.Ident]pkg.RelationKind {
	roles := map[*ast.Ident]pkg.RelationKind{}
	var mark func(ast.Expr, pkg.RelationKind)
	mark = func(e ast.Expr, kind pkg.RelationKind) {
		switch e := e.(type) {
		case *ast.Ident:
			roles[e] = kind
		case *ast.SelectorExpr:
			roles[e.Sel] = kind
		case *ast.ParenExpr:
			mark(e.X, kind)
		case *ast.IndexExpr:
			if kind == analysis.CallTargetUse {
				mark(e.X, kind)
			}
		case *ast.IndexListExpr:
			if kind == analysis.CallTargetUse {
				mark(e.X, kind)
			}
		}
	}

	var markType func(ast.Expr)
	markType = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Ident:
			roles[e] = pkg.UsesType
		case *ast.SelectorExpr:
			roles[e.Sel] = pkg.UsesType
		case *ast.ParenExpr:
			markType(e.X)
		case *ast.StarExpr:
			markType(e.X)
		case *ast.ArrayType:
			markType(e.Elt) // Array lengths are value reads.
		case *ast.MapType:
			markType(e.Key)
			markType(e.Value)
		case *ast.ChanType:
			markType(e.Value)
		case *ast.Ellipsis:
			markType(e.Elt)
		case *ast.IndexExpr:
			markType(e.X)
			markType(e.Index)
		case *ast.IndexListExpr:
			markType(e.X)
			for _, arg := range e.Indices {
				markType(arg)
			}
		case *ast.UnaryExpr:
			markType(e.X)
		case *ast.BinaryExpr:
			markType(e.X)
			markType(e.Y)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			markType(n.Type)
		case *ast.TypeSpec:
			markType(n.Type)
		case *ast.ValueSpec:
			markType(n.Type)
		case *ast.CompositeLit:
			markType(n.Type)
		case *ast.TypeAssertExpr:
			markType(n.Type)
		case *ast.CallExpr:
			mark(n.Fun, analysis.CallTargetUse)
			switch fun := n.Fun.(type) {
			case *ast.IndexExpr:
				markType(fun.Index)
			case *ast.IndexListExpr:
				for _, arg := range fun.Indices {
					markType(arg)
				}
			}
			if id, ok := n.Fun.(*ast.Ident); ok && id.Obj == nil && (id.Name == "make" || id.Name == "new") && len(n.Args) > 0 {
				markType(n.Args[0])
			}
		case *ast.AssignStmt:
			kind := pkg.Writes
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				kind = analysis.ReadWriteUse
			}
			for _, lhs := range n.Lhs {
				mark(lhs, kind)
			}
		case *ast.IncDecStmt:
			mark(n.X, analysis.ReadWriteUse)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				mark(n.Key, pkg.Writes)
				mark(n.Value, pkg.Writes)
			}
		}
		return true
	})
	return roles
}
