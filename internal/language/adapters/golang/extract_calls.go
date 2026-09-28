package golang

import (
	"go/ast"
	"go/token"
)

func goTypeHint(e ast.Expr) (string, string) {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name, ""
	case *ast.SelectorExpr:
		if module, ok := e.X.(*ast.Ident); ok {
			return e.Sel.Name, module.Name
		}
	case *ast.StarExpr:
		return goTypeHint(e.X)
	case *ast.ParenExpr:
		return goTypeHint(e.X)
	case *ast.UnaryExpr:
		return goTypeHint(e.X)
	case *ast.CompositeLit:
		return goTypeHint(e.Type)
	case *ast.IndexExpr:
		return goTypeHint(e.X)
	case *ast.IndexListExpr:
		return goTypeHint(e.X)
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "new" && len(e.Args) == 1 {
			return goTypeHint(e.Args[0])
		}
	}
	return "", ""
}

// Keep type syntax distinct from value syntax: T[K] may instantiate a type,
// while values[K] selects an element. Range variables are explicit projections.
type goAssignment struct {
	Expr   ast.Expr
	Result int
}

type goTypeExpression struct {
	Expr       ast.Expr
	IsType     bool
	Projection string
	Result     int
}

func goObjectTypeExpressions(id *ast.Ident, assignments map[*ast.Object][]goAssignment, seen map[*ast.Object]bool) []goTypeExpression {
	if id.Obj == nil || seen[id.Obj] {
		return nil
	}
	seen[id.Obj] = true
	defer delete(seen, id.Obj)
	var types []goTypeExpression
	add := func(e ast.Expr, isType bool, result int) {
		if alias, ok := e.(*ast.Ident); ok && alias.Obj != nil {
			types = append(types, goObjectTypeExpressions(alias, assignments, seen)...)
			return
		}
		types = append(types, goTypeExpression{Expr: e, IsType: isType, Result: result})
	}
	switch decl := id.Obj.Decl.(type) {
	case *ast.Field:
		add(decl.Type, true, 0)
	case *ast.FuncDecl, *ast.TypeSpec:
		types = append(types, goTypeExpression{Expr: id, IsType: true})
	case *ast.ValueSpec:
		if decl.Type != nil {
			add(decl.Type, true, 0)
		} else {
			for _, expr := range assignments[id.Obj] {
				add(expr.Expr, false, expr.Result)
			}
		}
	case *ast.AssignStmt:
		// go/parser represents := range bindings as a synthetic assignment whose
		// sole RHS is UnaryExpr{Op: RANGE}; do not treat its index as the element.
		if len(decl.Rhs) == 1 {
			if expr, ok := decl.Rhs[0].(*ast.UnaryExpr); ok && expr.Op == token.RANGE {
				for i, lhs := range decl.Lhs {
					if name, ok := lhs.(*ast.Ident); ok && name.Obj == id.Obj {
						projection := "range_key"
						if i == 1 {
							projection = "range_value"
						}
						types = append(types, goTypeExpression{Expr: expr.X, Projection: projection})
					}
				}
			}
		}
		for _, expr := range assignments[id.Obj] {
			add(expr.Expr, false, expr.Result)
		}
	}
	return types
}

func goObjectTypes(id *ast.Ident, assignments map[*ast.Object][]goAssignment, seen map[*ast.Object]bool) []CallTarget {
	var hints []CallTarget
	for _, expr := range goObjectTypeExpressions(id, assignments, seen) {
		if expr.Projection != "" || expr.Result != 0 {
			continue
		}
		name, module := goTypeHint(expr.Expr)
		if name != "" {
			hints = append(hints, CallTarget{ReceiverType: name, Module: module})
		}
	}
	return hints
}

func goCallableTargets(expr ast.Expr, basis string, seen map[*ast.Object]bool, assignments map[*ast.Object][]goAssignment) []CallTarget {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return goCallableTargets(e.X, basis, seen, assignments)
	case *ast.IndexExpr:
		return goCallableTargets(e.X, basis, seen, assignments)
	case *ast.IndexListExpr:
		return goCallableTargets(e.X, basis, seen, assignments)
	case *ast.Ident:
		if e.Obj == nil {
			return []CallTarget{{Name: e.Name, Kind: "function", Basis: basis}}
		}
		if seen[e.Obj] {
			return nil
		}
		seen[e.Obj] = true
		defer delete(seen, e.Obj)
		if _, ok := e.Obj.Decl.(*ast.FuncDecl); ok {
			return []CallTarget{{Name: e.Name, Kind: "function", Basis: basis}}
		}
		values := assignments[e.Obj]
		var out []CallTarget
		for _, value := range values {
			if value.Result == 0 {
				out = append(out, goCallableTargets(value.Expr, "callable_binding", seen, assignments)...)
			}
		}
		return out
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if id.Obj == nil {
				return []CallTarget{{Name: e.Sel.Name, Module: id.Name, Kind: "function", Basis: basis}}
			}
			types := goObjectTypes(id, assignments, map[*ast.Object]bool{})
			if len(types) == 0 {
				return []CallTarget{{Name: e.Sel.Name, Kind: "method", Basis: "method_name"}}
			}
			for i := range types {
				types[i].Name = e.Sel.Name
				types[i].Kind = "method"
				types[i].Basis = "receiver_type"
			}
			return types
		}
		typ, module := goTypeHint(e.X)
		if typ != "" {
			return []CallTarget{{Name: e.Sel.Name, ReceiverType: typ, Module: module, Kind: "method", Basis: "receiver_initialization"}}
		}
	}
	return nil
}

func enrichGoCallTargets(f *Facts, nodes map[Span]*ast.CallExpr, closures []Span, assignments map[*ast.Object][]goAssignment, describe func(ast.Expr) *GoType) {
	for i := range f.Calls {
		call := &f.Calls[i]
		hidden := false
		for _, span := range closures {
			if span.Start <= call.Start && span.End >= call.End {
				hidden = true
			}
		}
		if hidden || call.Builtin {
			continue
		}
		node := nodes[call.Span]
		if node == nil {
			continue
		}
		// Keep the existing static-function path. Only supplement dispatch it could
		// not represent, preserving lexical binding instead of bare-name guessing.
		if !call.Blocked {
			continue
		}
		call.Targets = goCallableTargets(node.Fun, "callable_binding", map[*ast.Object]bool{}, assignments)
		if selector, ok := goUnwrapInstantiation(node.Fun).(*ast.SelectorExpr); ok {
			call.Extension = usageHints{Receivers: goMemberReceiverTypes(selector.X, assignments, describe)}
		}
	}
}

func goAssignments(file *ast.File) map[*ast.Object][]goAssignment {
	assignments := map[*ast.Object][]goAssignment{}
	record := func(names []ast.Expr, values []ast.Expr) {
		tuple := false
		if len(values) == 1 && len(names) > 1 {
			_, tuple = values[0].(*ast.CallExpr)
		}
		for i, lhs := range names {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Obj == nil {
				continue
			}
			if tuple {
				assignments[id.Obj] = append(assignments[id.Obj], goAssignment{values[0], i})
			} else if i < len(values) {
				assignments[id.Obj] = append(assignments[id.Obj], goAssignment{values[i], 0})
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			record(n.Lhs, n.Rhs)
		case *ast.ValueSpec:
			names := make([]ast.Expr, len(n.Names))
			for i, name := range n.Names {
				names[i] = name
			}
			record(names, n.Values)
		}
		return true
	})
	return assignments
}

func goUnwrapInstantiation(expr ast.Expr) ast.Expr {
	for {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		default:
			return expr
		}
	}
}
