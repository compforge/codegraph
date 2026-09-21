package extract

import (
	"context"
	"fmt"
	"strings"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func analyzeOutline(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	outliner, err := gts.NewOutliner(tree.Language(), outlineQuery(entry),
		gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	if err != nil {
		f.Issues = append(f.Issues, Issue{Code: "outline_incomplete", Message: err.Error()})
	} else {
		declarations, report := outliner.OutlineTree(tree)
		if report.Declined() || report.Truncated || report.Omitted() > 0 || report.OwnerRuleMisses > 0 {
			f.Issues = append(f.Issues, Issue{Code: "outline_incomplete", Message: fmt.Sprintf("outline coverage: %+v", report)})
		}
		var flatten func([]gts.OutlineSymbol, int)
		flatten = func(items []gts.OutlineSymbol, parent int) {
			for _, item := range items {
				kind := item.Kind
				switch item.NodeType {
				case "struct_item", "struct_specifier":
					kind = "struct"
				case "type_alias_declaration":
					kind = "type_alias"
				}
				if kind == "function" && parent >= 0 && f.Declarations[parent].Kind == "class" {
					kind = "method"
				}
				span := Span{int(item.Range.StartByte), int(item.Range.EndByte)}
				if ConcreteKind(kind) == "" {
					f.Issues = append(f.Issues, Issue{Code: "unsupported_declaration", Message: kind, Span: span})
					flatten(item.Children, parent)
					continue
				}
				qualified := item.Name
				if parent >= 0 {
					qualified = f.Declarations[parent].QualifiedName + "." + qualified
				}
				// A nonlexical owner requires language-specific binding; do not
				// turn an unresolved owner name into a contains edge.
				if item.Owner != "" {
					qualified = item.Owner + "." + item.Name
					f.Issues = append(f.Issues, Issue{Code: "unresolved_owner", Message: item.Owner, Span: span})
				}
				index := len(f.Declarations)
				f.Declarations = append(f.Declarations, Declaration{Name: item.Name, QualifiedName: qualified, Kind: kind, Parent: parent, Span: span})
				flatten(item.Children, index)
			}
		}
		flatten(declarations, -1)
	}
	if !ModuleLanguage(f.Language) {
		// +why=`Grammar availability is not proof of language binding support`
		f.Issues = append(f.Issues, Issue{Code: "unsupported_resolution", Message: "declaration outline only; reference resolution and markers are not implemented for " + f.Language})
		return f, ctx.Err()
	}
	program, err := gts.NewFactProgram(tree.Language(), gts.FactImports|gts.FactCalls)
	if err != nil {
		return f, err
	}
	facts := program.Extract(tree)
	for _, imp := range facts.Imports {
		recorded := Import{Alias: imp.Alias, Path: imp.Path, From: imp.From, Relative: imp.Relative, Span: Span{int(imp.StartByte), int(imp.EndByte)}}
		// A from-import names the imported symbol; everything else pulls the
		// whole module. Wildcard and bare imports keep Names empty.
		if imp.Kind == "from_import" && !imp.Wildcard && imp.Name != "" {
			recorded.Names = []string{imp.Name}
		}
		f.Imports = append(f.Imports, recorded)
	}
	// Retain ancestry from downward traversal. Some grammars materialize hidden
	// nodes whose Parent chain ends before the visible lexical scope.
	callNodes := map[Span]*gts.Node{}
	var closures []Span
	walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{int(n.StartByte()), int(n.EndByte())}
		switch n.Type(tree.Language()) {
		case "call", "call_expression", "new_expression":
			callNodes[span] = n
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, span)
		}
	})
	for _, c := range facts.Calls {
		call := Call{Name: c.Name, Receiver: c.Receiver, Span: Span{int(c.StartByte), int(c.EndByte)}}
		var targets []Declaration
		for _, d := range f.Declarations {
			if d.Parent == -1 && d.Kind == "function" && d.Name == c.Name {
				targets = append(targets, d)
			}
		}
		n := callNodes[call.Span]
		if n == nil {
			call.Blocked = true
		} else {
			fn := n.ChildByFieldName("function", tree.Language())
			if fn == nil || fn.Type(tree.Language()) != "identifier" {
				call.Blocked = true
			}
			if len(targets) == 1 && hasBindingConflict(tree, n, targets[0], call.Name) {
				call.Blocked = true
			}
			// Anonymous bodies do not execute merely because their enclosing
			// declaration executes. Without a callable node, retain a gap instead
			// of crediting the outer function with an immediate call.
			for _, span := range closures {
				if span.Start <= call.Start && span.End >= call.End {
					call.Blocked = true
				}
			}
		}
		f.Calls = append(f.Calls, call)
	}
	enrichModuleSyntax(&f, tree)
	return f, ctx.Err()
}

func enrichModuleSyntax(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	var comments []Comment
	wrappers := map[int]int{}
	walk(tree.RootNode(), func(n *gts.Node) {
		typ := n.Type(lang)
		if typ == "export_statement" || typ == "decorated_definition" {
			for _, field := range []string{"declaration", "definition"} {
				if declaration := n.ChildByFieldName(field, lang); declaration != nil {
					wrappers[int(declaration.StartByte())] = int(n.StartByte())
				}
			}
		}
		if typ == "comment" {
			comments = append(comments, Comment{Text: n.Text(f.Source), Span: Span{int(n.StartByte()), int(n.EndByte())}})
		}
		if f.Language == "python" {
			return
		}
		if typ == "import_statement" || typ == "export_statement" {
			if src := n.ChildByFieldName("source", lang); src != nil {
				start := len(f.Imports)
				addModuleImport(f, src, lang)
				if len(f.Imports) > start {
					f.Imports[start].Names = importedNames(n, lang, f.Source)
				}
			} else if typ == "export_statement" {
				walk(n, func(child *gts.Node) {
					if child.Type(lang) != "export_specifier" {
						return
					}
					local := child.ChildByFieldName("name", lang)
					alias := child.ChildByFieldName("alias", lang)
					if local != nil && alias != nil {
						if f.Exports == nil {
							f.Exports = map[string]string{}
						}
						f.Exports[alias.Text(f.Source)] = local.Text(f.Source)
					}
				})
			}
		}
		if typ == "call_expression" {
			fn := n.ChildByFieldName("function", lang)
			if fn == nil {
				return
			}
			switch fn.Text(f.Source) {
			case "import", "require", "require.resolve":
				args := n.ChildByFieldName("arguments", lang)
				if args != nil && args.NamedChildCount() == 1 {
					addModuleImport(f, args.NamedChild(0), lang)
				}
			}
		}
	})
	for i := range f.Declarations {
		d := &f.Declarations[i]
		start := d.Start
		// Export/decorator wrappers belong to the declaration's documentation.
		if wrapper, ok := wrappers[start]; ok {
			start = wrapper
		}
		var attached []Comment
		for j := len(comments) - 1; j >= 0; j-- {
			c := comments[j]
			if c.End > start {
				continue
			}
			if strings.TrimSpace(string(f.Source[c.End:start])) != "" {
				break
			}
			lineStart := strings.LastIndexByte(string(f.Source[:c.Start]), '\n') + 1
			if strings.TrimSpace(string(f.Source[lineStart:c.Start])) != "" {
				break
			}
			attached = append(parseMarkers(c.Text, c.Start), attached...)
			start = c.Start
		}
		d.Comments = attached
	}
}

func addModuleImport(f *Facts, n *gts.Node, lang *gts.Language) {
	raw := n.Text(f.Source)
	span := Span{int(n.StartByte()), int(n.EndByte())}
	if n.Type(lang) != "string" || len(raw) < 2 || strings.Contains(raw, "\\") {
		f.Issues = append(f.Issues, Issue{Code: "dynamic_import", Message: "import target is not a plain string literal", Span: span})
		return
	}
	f.Imports = append(f.Imports, Import{Path: raw[1 : len(raw)-1], Span: span})
}

// importedNames lists the names an import or re-export statement binds from
// its source module, before caller aliases. Namespace and default imports bind
// the whole module, reported as nil.
func importedNames(n *gts.Node, lang *gts.Language, source []byte) []string {
	var names []string
	whole := false
	walk(n, func(child *gts.Node) {
		switch child.Type(lang) {
		case "import_specifier", "export_specifier":
			if name := child.ChildByFieldName("name", lang); name != nil {
				names = append(names, name.Text(source))
			}
		case "namespace_import":
			whole = true
		case "import_clause":
			for i := 0; i < child.NamedChildCount(); i++ {
				if child.NamedChild(i).Type(lang) == "identifier" {
					whole = true
				}
			}
		}
	})
	if whole {
		return nil
	}
	return names
}

func walk(root *gts.Node, visit func(*gts.Node)) {
	stack := []*gts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(n)
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.NamedChild(i))
		}
	}
}
