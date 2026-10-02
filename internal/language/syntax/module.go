package syntax

import (
	"context"
	"strings"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func (x ModuleExtractor) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	var err error
	f, err = Outline(ctx, f, tree, entry)
	if err != nil {
		return f, err
	}
	program, err := FactProgram(tree.Language(), gts.FactImports|gts.FactCalls)
	if err != nil {
		return f, err
	}
	facts := program.Extract(tree)
	for _, imp := range facts.Imports {
		recorded := Import{Alias: imp.Alias, Path: imp.Path, From: imp.From, Relative: imp.Relative, Binding: imp.Name, Span: Span{Start: int(imp.StartByte), End: int(imp.EndByte)}}
		// A from-import names the imported symbol; everything else pulls the
		// whole module. Wildcard and bare imports keep Names empty.
		if imp.Kind == "from_import" && !imp.Wildcard && imp.Name != "" {
			recorded.Names = []string{imp.Name}
		}
		f.Imports = append(f.Imports, recorded)
	}
	x.enrichModuleSyntax(&f, tree)
	x.lexical = x.Dialect.Lexical(&f, tree)
	f.Lexical = x.lexical
	// Retain ancestry from downward traversal. Some grammars materialize hidden
	// nodes whose Parent chain ends before the visible lexical scope.
	callNodes := map[Span]*gts.Node{}
	var closures []Span
	Walk(tree.RootNode(), func(n *gts.Node) {
		span := Span{Start: int(n.StartByte()), End: int(n.EndByte())}
		switch n.Type(tree.Language()) {
		case "call", "call_expression", "new_expression":
			callNodes[span] = n
		case "lambda", "arrow_function", "function_expression", "generator_function":
			closures = append(closures, x.moduleClosureSpan(n, tree.Language()))
		}
	})
	for _, c := range facts.Calls {
		call := Call{Name: c.Name, Receiver: c.Receiver, Span: Span{Start: int(c.StartByte), End: int(c.EndByte)}}
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
			if len(targets) == 1 && x.hasBindingConflict(tree, n, targets[0], call.Name) {
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
	x.extractModuleReferences(&f, tree)
	x.bindModuleUses(&f, tree)
	x.enrichModuleCallTargets(&f, tree)
	x.extractModuleTypeRelations(&f, tree)
	return f, ctx.Err()
}

func (x ModuleExtractor) enrichModuleSyntax(f *Facts, tree *gts.Tree) {
	lang := tree.Language()
	x.Dialect.Enrich(f, tree)
	var comments []Comment
	wrappers := map[int]int{}
	Walk(tree.RootNode(), func(n *gts.Node) {
		typ := n.Type(lang)
		if typ == "export_statement" || typ == "decorated_definition" {
			for _, field := range []string{"declaration", "definition"} {
				if declaration := n.ChildByFieldName(field, lang); declaration != nil {
					wrappers[int(declaration.StartByte())] = int(n.StartByte())
				}
			}
		}
		if typ == "comment" {
			comments = append(comments, Comment{Text: n.Text(f.Source), Span: Span{Start: int(n.StartByte()), End: int(n.EndByte())}})
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
			attached = append(ParseMarkers(c.Text, c.Start), attached...)
			start = c.Start
		}
		d.Comments = attached
	}
}
