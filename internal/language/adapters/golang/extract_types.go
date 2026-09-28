package golang

import (
	"go/ast"
	"go/token"
)

func extractGoTypeRelations(f *Facts, file *ast.File, fset *token.FileSet) {
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Assign.IsValid() {
			return true
		}
		typ, ok := spec.Type.(*ast.InterfaceType)
		if !ok {
			return true
		}
		owner := -1
		for i, d := range f.Declarations {
			if d.Start == fset.Position(spec.Pos()).Offset {
				owner = i
				break
			}
		}
		if owner < 0 {
			return true
		}
		for _, field := range typ.Methods.List {
			if len(field.Names) > 0 {
				continue
			}
			name, module := goTypeHint(field.Type)
			span := Span{Start: fset.Position(field.Pos()).Offset, End: fset.Position(field.End()).Offset}
			if name == "" {
				f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "interface type terms are not base-interface bindings", Subject: "relations", Relation: "extends", Span: span})
				continue
			}
			f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: owner, Name: name, Module: module, Kind: "extends", Basis: "interface_embedding", Span: span})
		}
		return true
	})
}
