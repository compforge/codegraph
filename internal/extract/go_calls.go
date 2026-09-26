package extract

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

func goObjectTypes(id *ast.Ident, assignments map[*ast.Object][]ast.Expr, seen map[*ast.Object]bool) []CallTarget {
	if id.Obj == nil || seen[id.Obj] {
		return nil
	}
	seen[id.Obj] = true
	defer delete(seen, id.Obj)
	var types []CallTarget
	add := func(e ast.Expr) {
		if alias, ok := e.(*ast.Ident); ok && alias.Obj != nil {
			types = append(types, goObjectTypes(alias, assignments, seen)...)
			return
		}
		name, module := goTypeHint(e)
		if name != "" {
			types = append(types, CallTarget{ReceiverType: name, Module: module})
		}
	}
	switch decl := id.Obj.Decl.(type) {
	case *ast.Field:
		add(decl.Type)
	case *ast.TypeSpec:
		types = append(types, CallTarget{ReceiverType: id.Name})
	case *ast.ValueSpec:
		if decl.Type != nil {
			add(decl.Type)
		} else {
			for _, expr := range assignments[id.Obj] {
				add(expr)
			}
		}
	case *ast.AssignStmt:
		for _, expr := range assignments[id.Obj] {
			add(expr)
		}
	}
	return types
}

func goCallableTargets(expr ast.Expr, basis string, seen map[*ast.Object]bool, assignments map[*ast.Object][]ast.Expr) []CallTarget {
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
			out = append(out, goCallableTargets(value, "callable_binding", seen, assignments)...)
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

func enrichGoCallTargets(f *Facts, file *ast.File, fset *token.FileSet, closures []Span) {
	nodes := map[int]*ast.CallExpr{}
	assignments := map[*ast.Object][]ast.Expr{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Obj != nil && i < len(n.Rhs) {
					assignments[id.Obj] = append(assignments[id.Obj], n.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if id.Obj != nil && i < len(n.Values) {
					assignments[id.Obj] = append(assignments[id.Obj], n.Values[i])
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			nodes[fset.Position(call.Pos()).Offset] = call
		}
		return true
	})
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
		node := nodes[call.Start]
		if node == nil {
			continue
		}
		// Keep the existing static-function path. Only supplement dispatch it could
		// not represent, preserving lexical binding instead of bare-name guessing.
		if !call.Blocked {
			continue
		}
		call.Targets = goCallableTargets(node.Fun, "callable_binding", map[*ast.Object]bool{}, assignments)
	}
}
