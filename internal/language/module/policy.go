package module

import (
	"github.com/compforge/codegraph/internal/analysis"
)

type Policy interface {
	ImportPaths(analysis.Facts, analysis.Import) []string
	Compatible(string) bool
	ImportConfidence(analysis.Import, int) analysis.Confidence
	ExportName(analysis.Facts, string) (string, bool)
	NestedImport(analysis.Import) bool
}
type NamespaceIndex struct {
	*analysis.Index
	policy Policy
}

func (x *NamespaceIndex) ModulePaths(f analysis.Facts, imp analysis.Import) []string {
	var out []string
	for _, p := range x.policy.ImportPaths(f, imp) {
		if target, ok := x.Files[p]; ok && x.policy.Compatible(target.Language) {
			out = append(out, p)
		}
	}
	return out
}
func (x *NamespaceIndex) ScopeFiles(f analysis.Facts) []string { return []string{f.Path} }
