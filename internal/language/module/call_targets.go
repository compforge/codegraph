package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveCallTargets(ctx context.Context, f analysis.Facts, call analysis.Call, files map[string]analysis.Facts, module string, methods *methodIndex, limit int) ([]Edge, error) {
	var edges []Edge
	source := analysis.SourceRef(f.Path, enclosingDeclaration(f, call.Span))
	seen := map[Ref]bool{}
	for _, hint := range call.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var targets []Ref
		inherited := map[Ref]bool{}

		binder := moduleBinder{ctx, files, limit - len(edges), methods.namespaces}
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
						classes = append(classes, analysis.SourceRef(f.Path, owner))
						break
					}
				}
			}
			lexicalClass := len(classes) > 0
			for i, d := range f.Declarations {
				if !lexicalClass && hint.Module == "" && d.Parent == -1 && d.Name == typeName && d.Kind == "class" {
					classes = append(classes, analysis.SourceRef(f.Path, i))
				}
			}
			imported, _, err := binder.useTargets(f, typeName, hint.Module, call.Span)
			if err != nil {
				return nil, err
			}
			for _, target := range imported {
				if target.IsDeclaration() && files[target.Path].Declarations[target.Declaration].Kind == "class" {
					classes = append(classes, target.Ref)
				}
			}
		}
		if hint.Kind == "constructor" {
			targets = append(targets, classes...)
		} else if len(classes) > 0 {
			found, err := methods.lookup(ctx, classes, hint.Name, limit-len(edges))
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
					targets = append(targets, analysis.SourceRef(f.Path, i))
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
			edges = append(edges, Edge{Source: source, Target: target, Kind: "calls", Confidence: "candidate", Basis: basis, Path: f.Path, Span: call.Span})
		}
	}
	return edges, nil
}
