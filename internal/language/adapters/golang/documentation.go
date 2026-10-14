package golang

import (
	"bytes"
	"go/ast"
	"go/token"

	"github.com/compforge/codegraph/internal/analysis"
)

func attachGoDocumentation(f *Facts, file *ast.File, fset *token.FileSet) {
	byName := map[int][]int{}
	for i, d := range f.Declarations {
		byName[d.NameSpan.Start] = append(byName[d.NameSpan.Start], i)
	}
	attach := func(names []*ast.Ident, doc *ast.CommentGroup) {
		for _, name := range names {
			for _, i := range byName[fset.Position(name.Pos()).Offset] {
				f.Declarations[i].Comments = comments(doc, fset)
				f.Declarations[i].Documentation = documentation(f, doc, fset)
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			attach([]*ast.Ident{n.Name}, n.Doc)
		case *ast.GenDecl:
			for _, spec := range n.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					attach([]*ast.Ident{spec.Name}, declarationDoc(spec.Doc, n.Doc, len(n.Specs)))
				case *ast.ValueSpec:
					attach(spec.Names, declarationDoc(spec.Doc, n.Doc, len(n.Specs)))
				}
			}
		case *ast.Field:
			if len(n.Names) > 0 {
				attach(n.Names, n.Doc)
				// Blank padding fields are absent from declaration facts; their
				// name-only query capture needs this enclosing syntax range.
				for _, name := range n.Names {
					if name.Name != "_" {
						continue
					}
					for _, i := range byName[fset.Position(name.Pos()).Offset] {
						f.Declarations[i].End = fset.Position(n.End()).Offset
						f.Declarations[i].SignatureSpan = Span{Start: fset.Position(n.Pos()).Offset, End: fset.Position(n.End()).Offset}
					}
				}
			} else {
				for i, d := range f.Declarations {
					if d.Start == fset.Position(n.Pos()).Offset && d.End == fset.Position(n.End()).Offset {
						f.Declarations[i].Comments = comments(n.Doc, fset)
						f.Declarations[i].Documentation = documentation(f, n.Doc, fset)
					}
				}
			}
		}
		return true
	})
}

func documentation(f *Facts, group *ast.CommentGroup, fset *token.FileSet) []analysis.Documentation {
	if group == nil {
		return nil
	}
	// go/ast strips carriage returns from Comment.Text, so Comment.End may
	// precede the original terminator. Bind with the AST, slice the source.
	last := fset.Position(group.List[len(group.List)-1].Pos()).Offset
	tail := f.Source[last:]
	end := len(tail)
	if bytes.HasPrefix(tail, []byte("/*")) {
		if at := bytes.Index(tail, []byte("*/")); at >= 0 {
			end = at + 2
		}
	} else if at := bytes.IndexByte(tail, '\n'); at >= 0 {
		end = at
		if end > 0 && tail[end-1] == '\r' {
			end--
		}
	}
	span := Span{Start: fset.Position(group.Pos()).Offset, End: last + end}
	return []analysis.Documentation{{Text: string(f.Source[span.Start:span.End]), Span: span}}
}

func declarationDoc(spec, group *ast.CommentGroup, groupSize int) *ast.CommentGroup {
	if spec == nil && groupSize == 1 {
		return group
	}
	return spec
}
