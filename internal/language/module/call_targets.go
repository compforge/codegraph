package module

import (
	"context"

	"github.com/compforge/codegraph/internal/analysis"
)

func resolveCallTargets(ctx context.Context, f analysis.Facts, call analysis.Call, files map[string]analysis.Facts, module string, methods *methodIndex, limit int) ([]Edge, error) {
	var edges []Edge
	source := analysis.SourceRef(f.Path, enclosingDeclaration(f, call.Span))
	type proofKey struct {
		Target     Ref
		Basis      string
		Confidence analysis.Confidence
	}
	seen := map[proofKey]bool{}
	for _, hint := range call.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var targets []bindingTarget
		inherited := map[Ref]bool{}

		binder := moduleBinder{ctx, files, limit - len(edges), methods.namespaces}
		typeName := hint.ReceiverType
		if hint.Kind == "constructor" {
			typeName = hint.Name
		}
		var classes []bindingTarget
		if typeName != "" {
			// self/this denotes its lexical class, including nested classes that do
			// not bind as a top-level name in this module.
			if hint.Basis == "lexical_receiver" {
				for owner := enclosingDeclaration(f, call.Span); owner >= 0; owner = f.Declarations[owner].Parent {
					d := f.Declarations[owner]
					if d.Kind == "class" && d.Name == typeName {
						classes = append(classes, bindingTarget{Ref: analysis.SourceRef(f.Path, owner), Confidence: analysis.Exact})
						break
					}
				}
			}
			lexicalClass := len(classes) > 0
			for i, d := range f.Declarations {
				if !lexicalClass && hint.Module == "" && d.Parent == -1 && d.Name == typeName && d.Kind == "class" {
					classes = append(classes, bindingTarget{Ref: analysis.SourceRef(f.Path, i), Confidence: analysis.Exact})
				}
			}
			imported, _, err := binder.useTargets(f, typeName, hint.Module, call.Span)
			if err != nil {
				return nil, err
			}
			for _, target := range imported {
				if target.IsDeclaration() && files[target.Path].Declarations[target.Declaration].Kind == "class" {
					classes = append(classes, target)
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
				targets = append(targets, target.BindingTarget)
				inherited[target.Ref] = target.Inherited
			}
		} else if hint.ReceiverType == "" {
			for i, d := range f.Declarations {
				if d.Kind == "method" && d.Name == hint.Name {
					targets = append(targets, bindingTarget{Ref: analysis.SourceRef(f.Path, i), Confidence: analysis.NameOnly})
				}
			}
		}
		for _, target := range targets {
			basis := hint.Basis
			confidence := analysis.Scoped
			if hint.Kind == "method" && hint.ReceiverType == "" {
				confidence = analysis.NameOnly
			}
			if inherited[target.Ref] {
				basis = "inherited_method"
			}
			// Deduplicate proofs, not targets: a later hint may supply stronger evidence.
			confidence = confidence.Weaker(target.Confidence)
			proof := proofKey{target.Ref, basis, confidence}
			if seen[proof] {
				continue
			}
			if len(edges) >= limit {
				return nil, ErrEdgeLimit
			}
			seen[proof] = true

			edges = append(edges, Edge{Source: source, Target: target.Ref, Kind: "calls", Confidence: confidence, Basis: basis, Path: f.Path, Span: call.Span})
		}
	}
	return edges, nil
}
