package golang

import (
	"go/ast"
	"go/token"

	gts "github.com/odvcencio/gotreesitter"
)

func extractGoTypeRelations(f *Facts, declarations []gts.DeclarationFact, file *ast.File, fset *token.FileSet, expressions map[Span]ast.Expr) {
	covered := map[Span]bool{}
	for _, d := range declarations {
		if d.Kind != "embed" {
			continue
		}
		span := Span{Start: int(d.StartByte), End: int(d.EndByte)}
		covered[span] = true
		for i, owner := range f.Declarations {
			if owner.Start == int(d.ContainerStartByte) && owner.End == int(d.ContainerEndByte) && owner.Kind == "interface" {
				_, module := goTypeHint(expressions[span])
				raw := d.Name
				f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: i, Name: raw, Module: module, Kind: "extends", Basis: "interface_embedding", Span: span})
			}
		}
	}
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
			span := Span{Start: fset.Position(field.Pos()).Offset, End: fset.Position(field.End()).Offset}
			if covered[span] {
				continue
			}
			name, module := goTypeHint(field.Type)
			if name == "" {
				f.Issues = append(f.Issues, Issue{Code: "unsupported_type_relation", Message: "interface type terms are not base-interface bindings", Subject: "relations", Relation: "extends", Span: span})
				continue
			}
			f.TypeRelations = append(f.TypeRelations, TypeRelation{Owner: owner, Name: name, Module: module, Kind: "extends", Basis: "interface_embedding", Span: span})
		}
		return true
	})
}
