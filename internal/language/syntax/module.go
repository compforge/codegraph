package syntax

import (
	"context"
	"strings"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func (x ModuleExtractor) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, gts.FactSet, error) {
	program, err := FactProgram(tree.Language(), gts.FactDefinitions|gts.FactDeclarations|gts.FactSignatures|gts.FactCallArguments|gts.FactImports|gts.FactCalls|gts.FactConstructorCalls, entry)
	if err != nil {
		return f, gts.FactSet{}, err
	}
	facts := program.Extract(tree)
	Declarations(&f, facts)
	x.signatures = facts.Signatures
	for _, imp := range facts.Imports {
		recorded := Import{Alias: imp.Alias, Path: imp.Path, From: imp.From, Relative: imp.Relative, Binding: imp.Name, Span: Span{Start: int(imp.StartByte), End: int(imp.EndByte)}}
		// A from-import names the imported symbol; everything else pulls the
		// whole module. Wildcard and bare imports keep Names empty.
		if imp.Kind == "from_import" && !imp.Wildcard && imp.Name != "" {
			recorded.Names = []string{imp.Name}
		}
		f.Imports = append(f.Imports, recorded)
	}
	x.enrichModuleSyntax(&f, tree, facts)
	CaptureSignatures(&f, tree)
	x.lexical = x.Dialect.Lexical(&f, tree, facts)
	f.Lexical = x.lexical
	x.extractModuleReferences(&f, tree)
	x.extractDecorators(&f, tree)
	x.bindModuleReferences(&f)
	x.extractModuleCalls(&f, tree, facts.Calls)
	x.extractModuleTypeRelations(&f, tree)
	return f, facts, ctx.Err()
}

func (x ModuleExtractor) enrichModuleSyntax(f *Facts, tree *gts.Tree, facts gts.FactSet) {
	lang := tree.Language()
	x.Dialect.Enrich(f, tree, facts)
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
