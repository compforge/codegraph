package codegraph

import (
	"context"
	"fmt"

	"github.com/compforge/codegraph/internal/analysis"
)

// A binding is located at a source use, even when other uses share its name.
// Owner participates in the key so a resolver cannot silently reparent a use.
type referenceKey struct {
	owner      string
	kind       RelationKind
	path       string
	start, end int
}

// publishReferences is the production-side handoff from extraction to graph
// facts. Uses exist before target binding; they never create external symbols.
// +spec=Every extracted call/reference has a source-use node even without a target; candidate targets retain the resolver's evidence.
// +why=Consumers must be able to inspect a partial graph without reading Facts or treating diagnostics as code facts.
func publishReferences(ctx context.Context, files map[string]analysis.Facts, ids map[analysis.Ref]string, nodes map[string]Node, relations map[string]Relation, opts Options) error {
	bindings := make(map[referenceKey][]Relation)
	evidenceCount := 0
	for _, r := range relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		evidenceCount += len(r.Evidence)
		if r.Kind == Calls || r.Kind == References || r.Kind == Extends || r.Kind == Implements || r.Kind == Decorates {
			key := referenceKey{r.Source, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte}
			bindings[key] = append(bindings[key], r)
			if r.Kind == Decorates {
				delete(relations, r.ID)
				evidenceCount -= len(r.Evidence)
			}
		}
	}
	addRelation := func(r Relation) error {
		r.ID = identity(r.Source, r.Target, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte)
		if _, exists := relations[r.ID]; exists {
			return nil
		}
		if len(relations) >= opts.MaxRelations {
			return &BuildBudgetError{Stage: "source-use", Resource: "MaxRelations", Used: len(relations), Adding: 1, Limit: opts.MaxRelations}
		}
		if evidenceCount+len(r.Evidence) > opts.MaxEvidence {
			return &BuildBudgetError{Stage: "source-use", Resource: "MaxEvidence", Used: evidenceCount, Adding: len(r.Evidence), Limit: opts.MaxEvidence}
		}
		relations[r.ID] = r
		evidenceCount += len(r.Evidence)
		return nil
	}
	addReference := func(f analysis.Facts, kind RelationKind, name, receiver string, span analysis.Span, owner int, direct analysis.Ref, decorated int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Source identity depends on this occurrence, never on available targets
		// or precision, so supplementing documents does not rename existing uses.
		id := "node:" + identity(f.Path, Reference, kind, span.Start, span.End, name, receiver)
		if _, exists := nodes[id]; exists {
			return nil
		}
		if len(nodes) >= opts.MaxNodes {
			return &BuildBudgetError{Stage: "source-use", Resource: "MaxNodes", Used: len(nodes), Adding: 1, Limit: opts.MaxNodes}
		}
		parent := ids[analysis.SourceRef(f.Path, owner)]
		if parent == "" {
			return fmt.Errorf("unpublished source-use owner: %s at %d", f.Path, span.Start)
		}
		loc := location(f, span)
		nodes[id] = Node{ID: id, Kind: Reference, ReferenceKind: ReferenceKind(kind), Name: name, Receiver: receiver, Language: f.Language, Location: &loc}
		if err := addRelation(Relation{Source: id, Target: parent, Kind: OccursIn, Location: loc,
			Confidence: Exact, Evidence: []Evidence{{Basis: "source_occurrence", Confidence: Exact}}}); err != nil {
			return err
		}
		if decorated >= 0 {
			if err := addRelation(Relation{Source: id, Target: ids[analysis.DeclarationRef(f.Path, decorated)], Kind: Decorates, Location: loc, Confidence: Exact, Evidence: []Evidence{{Basis: "source_modifier", Confidence: Exact}}}); err != nil {
				return err
			}
		}
		if target := ids[direct]; target != "" {
			return addRelation(Relation{Source: id, Target: target, Kind: References, Location: loc, Confidence: Exact, Evidence: []Evidence{{Basis: "local_import_binding", Confidence: Exact}}})
		}
		key := referenceKey{parent, kind, f.Path, span.Start, span.End}
		for _, binding := range bindings[key] {
			// Derive both graph views from the same resolver output. This is a
			// projection of evidence, never an independent name-based resolution.
			binding = cloneRelation(binding)
			binding.Source, binding.Kind = id, References
			if err := addRelation(binding); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range sortedFiles(files) {
		f := files[path]
		for _, call := range f.Calls {
			if err := addReference(f, Calls, call.Name, call.Receiver, call.Span, analysis.EnclosingDeclaration(f, call.Span), directImport(nodes, ids, f, call.Name, call.Receiver, call.Span, call.Imported), -1); err != nil {
				return err
			}
		}
		for _, ref := range f.References {
			kind := References
			if ref.Kind != "" {
				kind = RelationKind(ref.Kind)
			}
			decorated := -1
			if kind == Decorates {
				decorated = ref.Decorated
			}
			if err := addReference(f, kind, ref.Name, ref.Receiver, ref.Span, ref.Owner, directImport(nodes, ids, f, ref.Name, ref.Receiver, ref.Span, !ref.Bound), decorated); err != nil {
				return err
			}
		}
		for _, hint := range f.TypeRelations {
			if err := addReference(f, RelationKind(hint.Kind), hint.Name, hint.Module, hint.Span, hint.Owner, directImport(nodes, ids, f, hint.Name, hint.Module, hint.Span, !hint.Blocked), -1); err != nil {
				return err
			}
		}
	}
	return nil
}

func directImport(nodes map[string]Node, ids map[analysis.Ref]string, f analysis.Facts, name, receiver string, span analysis.Span, allowed bool) analysis.Ref {
	if allowed && receiver == "" {
		if ref, ok := analysis.LocalImport(f, name, span); ok {
			return ref
		}
		if f.Language == "go" {
			for i := range f.Imports {
				ref := analysis.ImportItemRef(f.Path, i, -1)
				item := nodes[ids[ref]]
				if item.Binding != nil && item.Binding.Form == "namespace" && item.Binding.LocalName == name {
					return ref
				}
			}
		}
	}
	return analysis.Ref{}
}
