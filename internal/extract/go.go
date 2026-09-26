package extract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

func enrichGo(f *Facts) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.Path, f.Source, parser.ParseComments)
	if err != nil {
		return err
	}
	f.Package = file.Name.Name
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	enrichGoDeclarations(f, file, fset)
	// File scope variables shadow package functions in Go. Keep them blocked,
	// rather than connecting a callback invocation to a same-named function.
	byStart := map[int]*ast.CallExpr{}
	var closures []Span
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncLit); ok {
			closures = append(closures, Span{offset(fn.Pos()), offset(fn.End())})
		}
		if c, ok := n.(*ast.CallExpr); ok {
			byStart[offset(c.Pos())] = c
		}
		return true
	})
	for i := range f.Calls {
		c := &f.Calls[i]
		// A closure's body is not an immediate call made by its outer function.
		// Until anonymous symbols are represented, preserve the coverage gap.
		for _, span := range closures {
			if span.Start <= c.Start && span.End >= c.End {
				c.Blocked = true
			}
		}
		if c.Blocked {
			continue
		}
		n := byStart[c.Start]
		if n == nil {
			c.Blocked = true
			continue
		}
		fun := n.Fun
		// Generic instantiations keep the identity of the called declaration.
		switch x := fun.(type) {
		case *ast.IndexExpr:
			fun = x.X
		case *ast.IndexListExpr:
			fun = x.X
		}
		switch x := fun.(type) {
		case *ast.Ident:
			c.Name = x.Name
			c.Receiver = ""
			if x.Obj != nil {
				_, function := x.Obj.Decl.(*ast.FuncDecl)
				c.Blocked = !function
			} else if obj := types.Universe.Lookup(x.Name); obj != nil {
				c.Builtin = true
			}
		case *ast.SelectorExpr:
			c.Name = x.Sel.Name
			if recv, ok := x.X.(*ast.Ident); ok {
				c.Receiver = recv.Name
				c.Blocked = recv.Obj != nil
			} else {
				c.Blocked = true
			}
		default:
			c.Blocked = true
		}
	}
	enrichGoCallTargets(f, file, fset, closures)
	extractGoReferences(f, file, fset)
	extractGoTypeRelations(f, file, fset)
	return nil
}

func receiverName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	}
	return "?"
}

func comments(group *ast.CommentGroup, fset *token.FileSet) []Comment {
	if group == nil {
		return nil
	}
	var out []Comment
	for _, c := range group.List {
		base := fset.Position(c.Pos()).Offset
		out = append(out, parseMarkers(c.Text, base)...)
	}
	return out
}

func parseMarkers(raw string, base int) []Comment {
	var out []Comment
	for _, line := range strings.SplitAfter(raw, "\n") {
		text := strings.TrimSpace(line)
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "//"), "/*"), "*/"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "#"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "*"))
		if strings.HasPrefix(text, "+") {
			kind, payload, ok := strings.Cut(text[1:], "=")
			if head, tail, found := strings.Cut(text[1:], ":"); found && (!ok || len(head) < len(kind)) {
				kind, payload, ok = head, tail, true
			}
			if ok && validMarker(kind) {
				payload = strings.TrimSpace(payload)
				if len(payload) >= 2 && payload[0] == '`' && payload[len(payload)-1] == '`' {
					payload = payload[1 : len(payload)-1]
				}
				if payload != "" {
					out = append(out, Comment{Kind: kind, Text: payload, Span: Span{base, base + len(strings.TrimSuffix(line, "\n"))}})
				}
			}
		}
		base += len(line)
	}
	return out
}

func validMarker(k string) bool {
	switch k {
	case "spec", "case", "rule", "link", "doc":
		return true
	}
	return false
}
