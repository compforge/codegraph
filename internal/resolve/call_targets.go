package resolve

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

// +why=`Syntax-based receiver and alias hints are candidates even when only one loaded target matches`
func resolveCallTargets(ctx context.Context, f extract.Facts, call extract.Call, files map[string]extract.Facts, module string, methods *methodIndex, limit int) ([]Edge, error) {
	var edges []Edge
	source := Ref{f.Path, enclosingDeclaration(f, call.Span)}
	seen := map[Ref]bool{}
	for _, hint := range call.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var targets []Ref
		inherited := map[Ref]bool{}
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

			if hint.Kind == "method" && hint.ReceiverType != "" {
				var roots []Ref
				for _, target := range goTypeTargets(f, hint.ReceiverType, hint.Module, files, methods.names, module) {
					roots = append(roots, target.Ref)
				}
				found, err := methods.lookup(ctx, roots, hint.Name, strings.HasSuffix(f.Path, "_test.go"), limit-len(edges))
				if err != nil {
					return nil, err
				}
				for _, target := range found {
					if hint.Module != "" && !exported(hint.Name) {
						continue
					}
					targets = append(targets, target.Ref)
					inherited[target.Ref] = target.Inherited
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
				// self/this denotes its lexical class, including nested classes that do
				// not bind as a top-level name in this module.
				if hint.Basis == "lexical_receiver" {
					for owner := enclosingDeclaration(f, call.Span); owner >= 0; owner = f.Declarations[owner].Parent {
						d := f.Declarations[owner]
						if d.Kind == "class" && d.Name == typeName {
							classes = append(classes, Ref{f.Path, owner})
							break
						}
					}
				}
				lexicalClass := len(classes) > 0
				for i, d := range f.Declarations {
					if !lexicalClass && hint.Module == "" && d.Parent == -1 && d.Name == typeName && d.Kind == "class" {
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
				found, err := methods.lookup(ctx, classes, hint.Name, false, limit-len(edges))
				if err != nil {
					return nil, err
				}
				for _, target := range found {
					targets = append(targets, target.Ref)
					inherited[target.Ref] = target.Inherited
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
			basis := hint.Basis
			if inherited[target] {
				basis = "inherited_method"
			}
			edges = append(edges, Edge{source, target, "calls", "candidate", basis, f.Path, call.Span})
		}
	}
	return edges, nil
}
