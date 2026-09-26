package extract

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	gts "github.com/odvcencio/gotreesitter"
)

func referenceOwner(f *Facts, span Span) int {
	owner, size := -1, len(f.Source)+1
	for i, d := range f.Declarations {
		if d.Start <= span.Start && d.End >= span.End && d.End-d.Start < size {
			owner, size = i, d.End-d.Start
		}
	}
	return owner
}

func extractGoReferences(f *Facts, file *ast.File, fset *token.FileSet) {
	excluded := map[*ast.Ident]bool{file.Name: true}
	receivers := map[*ast.Ident]string{}
	localSelectors := map[*ast.Ident]bool{}
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
			if recv, ok := n.X.(*ast.Ident); ok {
				receivers[n.Sel] = recv.Name
				if recv.Obj != nil {
					localSelectors[n.Sel] = true
				}
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || excluded[id] || id.Name == "_" || types.Universe.Lookup(id.Name) != nil && id.Obj == nil || id.Obj != nil && id.Obj.Pos() == id.Pos() {
			return true
		}
		span := Span{fset.Position(id.Pos()).Offset, fset.Position(id.End()).Offset}
		ref := Reference{Name: id.Name, Receiver: receivers[id], Span: span, Owner: referenceOwner(f, span), Target: -1}
		if localSelectors[id] {
			ref.Bound = true
		}
		if id.Obj != nil {
			ref.Bound = true
			start := -1
			switch decl := id.Obj.Decl.(type) {
			case *ast.FuncDecl:
				start = fset.Position(decl.Pos()).Offset
			case *ast.TypeSpec:
				start = fset.Position(decl.Pos()).Offset
			case *ast.ValueSpec:
				if len(decl.Names) == 1 {
					start = fset.Position(decl.Names[0].Pos()).Offset
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

// Extract before releasing the tree. Binding occurrences and comments are not
// references; property uses retain their receiver instead of becoming bare names.
func extractModuleReferences(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var visit func(*gts.Node, *gts.Node)
	visit = func(n, parent *gts.Node) {
		typ := n.Type(lang)
		if typ == "comment" || typ == "string" || typ == "import_statement" || typ == "import_from_statement" {
			return
		}
		if typ == "identifier" || typ == "property_identifier" || typ == "type_identifier" || typ == "shorthand_property_identifier" {
			binding := false
			receiver := ""
			if parent != nil {
				pt := parent.Type(lang)
				for _, field := range []string{"name", "left", "parameter", "pattern", "alias"} {
					if child := parent.ChildByFieldName(field, lang); child != nil && child.StartByte() <= n.StartByte() && child.EndByte() >= n.EndByte() {
						switch pt {
						case "function_definition", "function_declaration", "class_definition", "class_declaration", "type_alias_declaration", "interface_declaration", "method_definition", "variable_declarator", "assignment", "assignment_expression", "augmented_assignment", "augmented_assignment_expression", "pair", "enum_assignment", "enum_declaration", "method_signature", "property_signature", "export_specifier", "typed_parameter", "default_parameter", "required_parameter", "optional_parameter", "arrow_function":
							binding = true
						}
					}
				}
				switch pt {
				case "parameters", "formal_parameters", "lambda_parameters", "enum_body":
					binding = true
				case "attribute", "member_expression":
					for _, field := range []string{"attribute", "property"} {
						if child := parent.ChildByFieldName(field, lang); child != nil && child.StartByte() == n.StartByte() {
							for _, owner := range []string{"object"} {
								if recv := parent.ChildByFieldName(owner, lang); recv != nil {
									receiver = recv.Text(f.Source)
								}
							}
						}
					}
				}
			}
			if !binding {
				span := Span{int(n.StartByte()), int(n.EndByte())}
				ref := Reference{Name: n.Text(f.Source), Receiver: receiver, Span: span, Owner: referenceOwner(f, span), Target: -1}
				if receiver == "" {
					for i, d := range f.Declarations {
						if d.Parent == -1 && d.Name == ref.Name {
							ref.Bound = true
							if !hasBindingConflict(tree, n, d, ref.Name) {
								ref.Target = i
							}
							break
						}
					}
				}
				f.References = append(f.References, ref)
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), n)
		}
	}
	visit(tree.RootNode(), nil)
	sort.Slice(f.References, func(i, j int) bool { return f.References[i].Start < f.References[j].Start })
}
