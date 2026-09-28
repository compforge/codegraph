package syntax

import (
	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
)

type ModuleDialect interface {
	Enrich(*Facts, *gts.Tree)
	Lexical(*Facts, *gts.Tree) *analysis.Lexicon
	Parameters(*gts.Node, *gts.Language) (names []*gts.Node, expressions []ParameterExpression, handled bool)
	Self(*Facts, *gts.Tree, int, string) bool
	Bases(*gts.Node, *gts.Language, func(*gts.Node, string))
	Closure(*gts.Node, *gts.Language) Span
}
type ModuleExtractor struct {
	Dialect ModuleDialect
	lexical *analysis.Lexicon
}

func NodeContains(outer, inner *gts.Node) bool {
	return outer != nil && inner != nil && outer.StartByte() <= inner.StartByte() && outer.EndByte() >= inner.EndByte()
}

// ParameterExpression retains downward ancestry even for grammars with hidden parents.
type ParameterExpression struct{ Node, Parameter *gts.Node }
