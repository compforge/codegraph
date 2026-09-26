package resolve

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

func resolveReferences(ctx context.Context, files map[string]extract.Facts, names []string, module string, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	for _, name := range names {
		f := files[name]
		for _, r := range f.References {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			targets := []Ref{}
			confidence, basis := "candidate", "lexical_name"
			if r.Bound {
				if r.Target >= 0 {
					targets = append(targets, Ref{name, r.Target})
					confidence, basis = "exact", "lexical_binding"
				}
			} else if r.Receiver != "" && f.Language == "go" {
				for _, imp := range f.Imports {
					dir, local := ImportDir(module, imp.Path)
					if !local {
						continue
					}
					for _, targetPath := range names {
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
								targets = append(targets, Ref{targetPath, i})
							}
						}
					}
				}
				basis = "imported_name"
			} else if r.Receiver == "" {
				for _, targetPath := range names {
					target := files[targetPath]
					// Go package scope can cross files. Other languages stay file-local until
					// explicit module bindings are resolved; never pair across languages.
					if targetPath != name && (f.Language != "go" || target.Language != "go" || path.Dir(targetPath) != path.Dir(name) || target.Package != f.Package || strings.HasSuffix(targetPath, "_test.go") && !strings.HasSuffix(name, "_test.go")) {
						continue
					}
					for i, d := range target.Declarations {
						if d.Parent == -1 && d.Receiver == "" && d.Name == r.Name {
							targets = append(targets, Ref{targetPath, i})
						}
					}
				}
			}
			for _, target := range targets {
				if len(edges) >= limit {
					return nil, nil, ErrEdgeLimit
				}
				edges = append(edges, Edge{Ref{name, r.Owner}, target, "references", confidence, basis, name, r.Span})
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
				issues = append(issues, Issue{name, "unresolved_reference", r.Name, "references", r.Span})
			}
		}
	}
	return edges, issues, nil
}
