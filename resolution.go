package codegraph

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

// ResolutionContext supplies snapshot-specific repository knowledge. It never
// loads files: only supplied Facts can become graph endpoints. Omitted imports
// use language defaults; an explicit empty Targets list means known unresolved.
type ResolutionContext struct {
	// GoModules maps snapshot-relative module roots to module import paths.
	GoModules map[string]string
	// Imports supplies lexical module targets for Python and JavaScript/TypeScript.
	Imports []ImportResolution
}

// ImportResolution identifies a lexical import occurrence and supplies its
// candidate module documents. Targets exclude merely incidental dependencies
// (for example Python ancestor initializers). Confidence caps every derived
// binding; it is required even when no target exists.
type ImportResolution struct {
	Document   string
	Import     FactImport
	Targets    []string
	Confidence Confidence
	Basis      string
}

func compileResolution(context ResolutionContext, limit int) (analysis.ResolutionContext, error) {
	out := analysis.ResolutionContext{GoModules: map[string]string{}, Imports: map[analysis.ImportKey]analysis.ImportResolution{}}
	used := len(context.GoModules)
	for root, module := range context.GoModules {
		if !fs.ValidPath(root) || strings.TrimSpace(module) == "" {
			return out, fmt.Errorf("invalid Go module mapping %q", root)
		}
		out.GoModules[root] = module
	}
	for _, entry := range context.Imports {
		used += 1 + len(entry.Targets)
		if used > limit {
			return out, fmt.Errorf("%w: resolution context", ErrBuildBudget)
		}
		if !fs.ValidPath(entry.Document) || entry.Import.Location.StartByte < 0 || !entry.Confidence.Valid() {
			return out, fmt.Errorf("invalid import resolution for %q", entry.Document)
		}
		targets := make([]string, 0, len(entry.Targets))
		seen := map[string]bool{}
		for _, p := range entry.Targets {
			if !fs.ValidPath(p) {
				return out, fmt.Errorf("invalid import target %q", p)
			}
			if !seen[p] {
				targets = append(targets, p)
				seen[p] = true
			}
		}
		key := analysis.ImportKey{Document: entry.Document, Start: entry.Import.Location.StartByte, Path: entry.Import.Path, From: entry.Import.From, Binding: entry.Import.Binding}
		if _, exists := out.Imports[key]; exists {
			return out, fmt.Errorf("duplicate import resolution for %s at %d", entry.Document, key.Start)
		}
		out.Imports[key] = analysis.ImportResolution{Targets: targets, Confidence: analysis.Confidence(entry.Confidence), Basis: entry.Basis}
	}
	if used > limit {
		return out, fmt.Errorf("%w: resolution context", ErrBuildBudget)
	}
	return out, nil
}

// SetResolutionContext replaces repository knowledge before the next Build.
// It copies all input collections; previously published graphs remain unchanged.
func (b *Builder) SetResolutionContext(context ResolutionContext) error {
	resolution, err := compileResolution(context, b.opts.MaxEvidence)
	if err != nil {
		return err
	}
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.resolution = resolution
	return nil
}
