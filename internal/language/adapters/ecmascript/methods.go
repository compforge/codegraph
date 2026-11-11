package ecmascript

import (
	"context"

	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// +tmp:text=`TypeScript/TSX modifier methods can still report async as the name; repair with upstream query captures`,until=`Declaration/signature facts pass TestECMAScriptGeneratorAndPrivateDeclarations without repair`
// +case=TestECMAScriptGeneratorAndPrivateDeclarations
func repairTypeScriptMethodNames(f *Facts, tree *gts.Tree) {
	entry := *grammars.DetectLanguageByName(f.Language)
	params := "[(formal_parameters) (type_parameters)]"
	entry.TagsQuery = "(method_definition [(property_identifier) (private_property_identifier)] @name . (comment)* . " + params + ") @definition.method"
	methods, err := syntax.Outline(context.Background(), Facts{Language: f.Language, Source: f.Source}, tree, entry)
	if err != nil {
		return
	}
	for _, issue := range methods.Issues {
		if issue.Code == "outline_incomplete" {
			f.Issues = append(f.Issues, issue)
		}
	}
	for _, method := range methods.Declarations {
		for i := range f.Declarations {
			d := &f.Declarations[i]
			if d.Span == method.Span && d.Kind == "method" {
				d.Name, d.NameSpan = method.Name, method.NameSpan
			}
		}
	}
	syntax.AssignDeclarationParents(f.Declarations, f.Language)
}
