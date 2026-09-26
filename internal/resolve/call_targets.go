package resolve

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

// +why=`Syntax-based receiver and alias hints are candidates even when only one loaded target matches`
func resolveCallTargets(ctx context.Context, f extract.Facts, call extract.Call, files map[string]extract.Facts, module string, limit int) ([]Edge, error) {
	var edges []Edge
	source := Ref{f.Path, enclosingDeclaration(f, call.Span)}
	seen := map[Ref]bool{}
	for _, hint := range call.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var targets []Ref
		if f.Language == "go" {
			dir := path.Dir(f.Path)
			if hint.Module != "" {
				dir = ""
				for _, imp := range f.Imports {
					alias := imp.Alias
					if alias == "" {
						alias = path.Base(imp.Path)
						if imported, ok := ImportDir(module, imp.Path); ok {
							for p, target := range files {
								if path.Dir(p) == imported && target.Language == "go" && target.Package == hint.Module {
									alias = hint.Module
								}
							}
						}
					}
					if alias == hint.Module {
						if imported, ok := ImportDir(module, imp.Path); ok {
							dir = imported
						}
					}
				}
			}
			for name, target := range files {
				if dir == "" || target.Language != "go" || path.Dir(name) != dir || hint.Module == "" && target.Package != f.Package || strings.HasSuffix(name, "_test.go") && !strings.HasSuffix(f.Path, "_test.go") {
					continue
				}
				for i, d := range target.Declarations {
					if d.Name != hint.Name {
						continue
					}
					if hint.Module != "" && !exported(d.Name) {
						continue
					}
					if hint.Kind == "function" && d.Kind == "function" && d.Parent == -1 {
						targets = append(targets, Ref{name, i})
					}
					if hint.Kind == "method" && d.Kind == "method" && (hint.ReceiverType == "" || d.Receiver == hint.ReceiverType || d.Parent >= 0 && target.Declarations[d.Parent].Name == hint.ReceiverType) {
						targets = append(targets, Ref{name, i})
					}
				}
			}
		} else {
			binder := moduleBinder{ctx, files, limit - len(edges)}
			typeName := hint.ReceiverType
			if hint.Kind == "constructor" {
				typeName = hint.Name
			}
			var classes []Ref
			if typeName != "" {
				for i, d := range f.Declarations {
					if hint.Module == "" && d.Parent == -1 && d.Name == typeName && d.Kind == "class" {
						classes = append(classes, Ref{f.Path, i})
					}
				}
				imported, _, err := binder.useTargets(f, typeName, hint.Module, call.Span)
				if err != nil {
					return nil, err
				}
				for _, target := range imported {
					if target.Declaration >= 0 && files[target.Path].Declarations[target.Declaration].Kind == "class" {
						classes = append(classes, target.Ref)
					}
				}
			}
			if hint.Kind == "constructor" {
				targets = append(targets, classes...)
			} else if len(classes) > 0 {
				for _, class := range classes {
					for i, d := range files[class.Path].Declarations {
						if d.Kind == "method" && d.Name == hint.Name && d.Parent == class.Declaration {
							targets = append(targets, Ref{class.Path, i})
						}
					}
				}
			} else if hint.ReceiverType == "" {
				for i, d := range f.Declarations {
					if d.Kind == "method" && d.Name == hint.Name {
						targets = append(targets, Ref{f.Path, i})
					}
				}
			}
		}
		for _, target := range targets {
			if seen[target] {
				continue
			}
			if len(edges) >= limit {
				return nil, ErrEdgeLimit
			}
			seen[target] = true
			edges = append(edges, Edge{source, target, "calls", "candidate", hint.Basis, f.Path, call.Span})
		}
	}
	return edges, nil
}
