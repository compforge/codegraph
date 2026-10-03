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
	for _, p := range x.ImportPaths(f, imp) {
		if target, ok := x.Files[p]; ok && x.policy.Compatible(target.Language) {
			out = append(out, p)
		}
	}
	return out
}
func (x *NamespaceIndex) ScopeFiles(f analysis.Facts) []string { return []string{f.Path} }

func (x *NamespaceIndex) ImportPaths(f analysis.Facts, imp analysis.Import) []string {
	if imp.Dynamic {
		return nil
	}
	if resolved, ok := x.Resolution.Import(f, imp); ok {
		return resolved.Targets
	}
	return x.policy.ImportPaths(f, imp)
}
func (x *NamespaceIndex) ImportConfidence(f analysis.Facts, imp analysis.Import, count int) analysis.Confidence {
	resolved, ok := x.Resolution.Import(f, imp)
	if ok {
		count = max(count, len(resolved.Targets))
	}
	confidence := x.policy.ImportConfidence(imp, count)
	if ok {
		confidence = confidence.Weaker(resolved.Confidence)
	}
	return confidence
}
func (x *NamespaceIndex) ImportBasis(f analysis.Facts, imp analysis.Import) string {
	if resolved, ok := x.Resolution.Import(f, imp); ok && resolved.Basis != "" {
		return resolved.Basis
	}
	return "source_module"
}
