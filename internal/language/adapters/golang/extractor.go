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
	program, err := syntax.FactProgram(tree.Language(), gts.FactDefinitions|gts.FactDeclarations|gts.FactSignatures|gts.FactCallArguments|gts.FactCalls|gts.FactImports, entry)
	if err != nil {
		return f, err
	}
	facts := program.Extract(tree)
	// Declaration rules omit blank names, including padding fields.
	blankEntry := entry
	blankEntry.TagsQuery = `(field_declaration name: (field_identifier) @name @definition.field (#eq? @name "_"))`
	blanks, err := syntax.Outline(ctx, Facts{Language: f.Language, Source: f.Source}, tree, blankEntry)
	if err != nil {
		return f, err
	}
	f.Declarations = append(f.Declarations, blanks.Declarations...)
	syntax.Declarations(&f, facts)
	syntax.CaptureSignatures(&f, tree)
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
	if err := enrichGo(&f, facts); err != nil {
		return f, err
	}
	markGoEntrypoints(&f, facts.Signatures)
	return f, ctx.Err()
}
