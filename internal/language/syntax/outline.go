package syntax

import (
	"context"
	"github.com/compforge/codegraph/internal/analysis"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func Outline(ctx context.Context, f Facts, tree *gts.Tree, entry grammars.LangEntry) (Facts, error) {
	outliner, err := gts.NewOutliner(tree.Language(), entry.TagsQuery,
		gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	if err != nil {
		f.Issues = append(f.Issues, Issue{Code: "outline_incomplete", Message: err.Error(), Subject: "declarations", Span: Span{End: len(f.Source)}})
	} else {
		declarations, report := outliner.OutlineTree(tree)
		// Duplicate candidates retain the same declaration and are not missing
		// information. All other omissions remain visible at document scope:
		// upstream reports counts but cannot identify the dropped source ranges.
		if report.Declined() || report.Truncated || report.Omitted() > report.OmittedDuplicate || report.OwnerRuleMisses > 0 {
			f.Issues = append(f.Issues, Issue{Code: "outline_incomplete", Message: "declaration query omitted candidates or could not finish", Subject: "declarations", Span: Span{End: len(f.Source)},
				Outline: &OutlineCoverage{
					Symbols: report.Symbols, OmittedNoName: report.OmittedNoName, OmittedDuplicate: report.OmittedDuplicate,
					OmittedNameConflict: report.OmittedNameConflict, OmittedConflict: report.OmittedConflict,
					OmittedOverlap: report.OmittedOverlap, OmittedInvalidNameRange: report.OmittedInvalidNameRange,
					OmittedMultipleDefinitions: report.OmittedMultipleDefinitions, OwnerRuleMisses: report.OwnerRuleMisses,
					DeclineReason: report.DeclineReason, Truncated: report.Truncated,
				}})
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
				span := Span{Start: int(item.Range.StartByte), End: int(item.Range.EndByte)}
				if analysis.ConcreteKind(kind) == "" {
					f.Issues = append(f.Issues, Issue{Code: "unsupported_declaration", Message: kind, Subject: "declarations", Span: span})
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
					f.Issues = append(f.Issues, Issue{Code: "unresolved_owner", Message: item.Owner, Subject: "relations", Relation: "contains", Span: span})
				}
				index := len(f.Declarations)
				f.Declarations = append(f.Declarations, Declaration{Name: item.Name, QualifiedName: qualified, Kind: kind, Parent: parent, Span: span})
				flatten(item.Children, index)
			}
		}
		flatten(declarations, -1)
	}
	return f, ctx.Err()
}
