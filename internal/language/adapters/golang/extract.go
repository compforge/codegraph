package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

func enrichGo(f *Facts, facts gts.FactSet) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.Path, f.Source, parser.ParseComments)
	if err != nil {
		return err
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	attachGoDocumentation(f, file, fset)
	embedded := map[int]bool{}
	for _, d := range facts.Declarations {
		if d.Embedded && d.Kind == "field" {
			embedded[int(d.NameStartByte)] = true
		}
	}
	// Graph locations retain distinct per-name identities for Go siblings.
	for i := range f.Declarations {
		d := &f.Declarations[i]
		if d.Kind == "field" && !embedded[d.NameSpan.Start] || d.Kind == "variable" || d.Kind == "constant" {
			d.Start = d.NameSpan.Start
		}
	}
	// File scope variables shadow package functions in Go. Keep them blocked,
	// rather than connecting a callback invocation to a same-named function.
	// Nested calls may share a start offset; their full spans identify them.
	callNodes := map[Span]*ast.CallExpr{}
	expressions := map[Span]ast.Expr{}
	var closures []Span
	ast.Inspect(file, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			expressions[Span{Start: offset(e.Pos()), End: offset(e.End())}] = e
		}
		if fn, ok := n.(*ast.FuncLit); ok {
			closures = append(closures, Span{Start: offset(fn.Pos()), End: offset(fn.End())})
		}
		if c, ok := n.(*ast.CallExpr); ok {
			callNodes[Span{Start: offset(c.Pos()), End: offset(c.End())}] = c
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
		n := callNodes[c.Span]
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
	assignments := goAssignments(file)
	describe := goTypeDescriber(f, fset)
	applyGoSignatures(f, facts.Signatures, expressions, describe)
	enrichGoCallTargets(f, callNodes, closures, assignments, describe, facts.CallArguments, expressions)
	extractGoReferences(f, file, fset, assignments)
	extractGoTypeRelations(f, facts.Declarations, file, fset, expressions)
	return nil
}

func comments(group *ast.CommentGroup, fset *token.FileSet) []Comment {
	if group == nil {
		return nil
	}
	var out []Comment
	for _, c := range group.List {
		base := fset.Position(c.Pos()).Offset
		out = append(out, syntax.ParseMarkers(c.Text, base)...)
	}
	return out
}
