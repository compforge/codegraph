package golang

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

type Ref = analysis.Ref
type Edge = analysis.Edge
type Gap = analysis.Gap
type bindingTarget = analysis.BindingTarget

var ErrEdgeLimit = analysis.ErrEdgeLimit

func uniqueBindingTargets(t []bindingTarget) []bindingTarget { return analysis.UniqueBindingTargets(t) }
func enclosingDeclaration(f analysis.Facts, s analysis.Span) int {
	return analysis.EnclosingDeclaration(f, s)
}

type methodIndex struct {
	namespaces *NamespaceIndex
	names      []string
	shared     *analysis.MethodIndex
}

func (m *methodIndex) lookup(ctx context.Context, roots []Ref, name string, includeTests bool, limit int) ([]analysis.MethodTarget, error) {
	return m.shared.Lookup(ctx, roots, name, func(r Ref) bool { return includeTests || !strings.HasSuffix(r.Path, "_test.go") }, limit)
}
