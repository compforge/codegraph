package codegraph

import (
	"context"
	"slices"
)

func hasDiagnostic(r BuildReport, code string) bool {
	return slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == code })
}

// addDocumentsSync keeps existing build-contract tests focused on the final
// published batch while production callers use AddDocuments followed by Wait.
func (g *Graph) addDocumentsSync(ctx context.Context, docs ...Document) (BuildReport, error) {
	if err := g.AddDocuments(ctx, docs...); err != nil {
		return g.Report(), err
	}
	return g.Wait(ctx)
}
