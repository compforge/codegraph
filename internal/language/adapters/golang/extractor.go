package golang

import (
	"bytes"
	"context"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Adapter struct{}

func (Adapter) Extract(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	// Go declarations use semantic enrichment; navigation keeps the upstream
	// lexical outline, including out-of-line receiver names, from the same tree.
	entry.TagsQuery = grammars.ResolveTagsQuery(entry)
	f = syntax.CaptureOutline(f, tree, entry)
	program, err := syntax.FactProgram(tree.Language(), gts.FactDefinitions|gts.FactCalls|gts.FactImports)
	if err != nil {
		return f, err
	}
	facts := program.Extract(tree)
	for _, d := range facts.Definitions {
		f.Declarations = append(f.Declarations, Declaration{Name: d.Name, QualifiedName: d.Name, Kind: d.Kind, Span: Span{Start: int(d.StartByte), End: int(d.EndByte)}})
	}
	for _, c := range facts.Calls {
		f.Calls = append(f.Calls, Call{Name: c.Name, Receiver: c.Receiver, Span: Span{Start: int(c.StartByte), End: int(c.EndByte)}})
	}
	for _, i := range facts.Imports {
		if i.Kind == "package" {
			f.Package = i.Name
			f.PackageSpan = Span{Start: int(i.StartByte), End: int(i.EndByte)}
			continue
		}
		f.Imports = append(f.Imports, Import{Alias: i.Alias, Path: i.Path, Binding: i.Name, Span: Span{Start: int(i.StartByte), End: int(i.EndByte)}})
	}
	if bytes.Contains(f.Source, []byte("//go:embed")) {
		f.Issues = append(f.Issues, Issue{Code: "unsupported_resource", Message: "go:embed dependencies are not resolved", Subject: "resources", Span: Span{End: len(f.Source)}})
	}
	if err := enrichGo(&f); err != nil {
		return f, err
	}
	return f, ctx.Err()
}
