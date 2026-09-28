package golang

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveReferences(ctx context.Context, files map[string]analysis.Facts, names []string, module string, methods *methodIndex, limit int) ([]Edge, []Gap, error) {
	var edges []Edge
	var issues []Gap
	for _, name := range names {
		f := files[name]
		for _, r := range f.References {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			targets := []Ref{}
			confidence, basis := analysis.Scoped, "lexical_name"
			if usage(r.Extension).Key != nil {
				var ordinary bool
				var err error
				targets, ordinary, err = goCompositeKeyTargets(ctx, f, r, files, methods.namespaces, module, limit-len(edges))
				if err != nil {
					return nil, nil, err
				}
				if !ordinary {
					for _, target := range targets {
						if len(edges) >= limit {
							return nil, nil, ErrEdgeLimit
						}
						edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target, Kind: "references", Confidence: "scoped", Basis: "composite_field", Path: name, Span: r.Span})
					}
					if len(targets) == 0 {
						issues = append(issues, Gap{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: "references", Span: r.Span})
					}
					continue
				}
			}
			if r.Member {
				found, err := goMemberReferenceTargets(ctx, f, r, files, module, methods, limit-len(edges))
				if err != nil {
					return nil, nil, err
				}
				for _, target := range found {
					edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target, Kind: "references", Confidence: "scoped", Basis: "receiver_type", Path: name, Span: r.Span})
				}
				if len(found) == 0 {
					issues = append(issues, Gap{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: "references", Span: r.Span})
				}
				continue
			}
			if state := r.BindingState(); state != analysis.NotApplicable {
				if state == analysis.Bound {
					targets = append(targets, analysis.SourceRef(name, r.Target))
					confidence, basis = "exact", "lexical_binding"
				}
			} else if r.Receiver != "" && f.Language == "go" {
				for _, imp := range f.Imports {
					dir, local := ImportDir(module, imp.Path)
					if !local {
						continue
					}
					for _, targetPath := range methods.namespaces.GoImportFiles(module, imp.Path) {
						target := files[targetPath]
						alias := imp.Alias
						if alias == "" {
							alias = target.Package
						}
						if target.Language != "go" || path.Dir(targetPath) != dir || alias != r.Receiver || strings.HasSuffix(targetPath, "_test.go") {
							continue
						}
						for i, d := range target.Declarations {
							if d.Parent == -1 && d.Receiver == "" && d.Name == r.Name && exported(d.Name) {
								targets = append(targets, analysis.SourceRef(targetPath, i))
							}
						}
					}
				}
				basis = "imported_name"
			} else if r.Receiver == "" {
				for _, targetPath := range methods.namespaces.ScopeFiles(f) {
					target := files[targetPath]
					// Go package scope can cross files. Other languages stay file-local until
					// explicit module bindings are resolved; never pair across languages.
					if targetPath != name && (f.Language != "go" || target.Language != "go" || path.Dir(targetPath) != path.Dir(name) || target.Package != f.Package || strings.HasSuffix(targetPath, "_test.go") && !strings.HasSuffix(name, "_test.go")) {
						continue
					}
					for i, d := range target.Declarations {
						if d.Parent == -1 && d.Receiver == "" && d.Name == r.Name {
							targets = append(targets, analysis.SourceRef(targetPath, i))
						}
					}
				}
			}
			for _, target := range targets {
				if len(edges) >= limit {
					return nil, nil, ErrEdgeLimit
				}
				edges = append(edges, Edge{Source: analysis.SourceRef(name, r.Owner), Target: target, Kind: "references", Confidence: confidence, Basis: basis, Path: name, Span: r.Span})
			}
			// Imported module identifiers are not declaration references themselves.
			moduleName := false
			if f.Language == "go" && r.Receiver == "" {
				for _, imp := range f.Imports {
					alias := imp.Alias
					if alias == "" {
						alias = path.Base(imp.Path)
					}
					if alias == r.Name {
						moduleName = true
					}
				}
			}
			if len(targets) == 0 && !r.Bound && !moduleName {
				issues = append(issues, Gap{Path: name, Code: "unresolved_reference", Reference: r.Name, Relation: "references", Span: r.Span})
			}
		}
	}
	return edges, issues, nil
}
