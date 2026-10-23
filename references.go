package codegraph

import (
	"context"
	"fmt"

	"github.com/compforge/codegraph/internal/analysis"
)

// A binding is located at a source use, even when other uses share its name.
// Owner participates in the key so a resolver cannot silently reparent a use.
type referenceKey struct {
	owner      analysis.Ref
	kind       RelationKind
	path       string
	start, end int
}

// publishReferences derives occurrence views directly from semantic evidence.
// +spec=Unpublished owners and import items do not change occurrence identity or binding.
func (a *graphAssembler) publishReferences(ctx context.Context) error {
	a.stage = "source-use"
	files, ids, nodes := a.files, a.ids, a.nodes
	bindings := map[referenceKey][]analysis.Edge{}
	for _, e := range a.index.Edges {
		switch RelationKind(e.Kind) {
		case Calls, References, Extends, Implements, Decorates:
			key := referenceKey{e.Source, RelationKind(e.Kind), e.Path, e.Span.Start, e.Span.End}
			bindings[key] = append(bindings[key], e)
		}
	}
	addRelation := a.addRelation
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

		ownerRef := analysis.SourceRef(f.Path, owner)
		parent := ids[ownerRef]
		if _, exists := a.index.Entities[ownerRef]; !exists {
			return fmt.Errorf("unpublished source-use owner: %s at %d", f.Path, span.Start)
		}
		loc := location(f, span)
		if err := a.addNode(Node{ID: id, Kind: Reference, ReferenceKind: ReferenceKind(kind), Name: name, Receiver: receiver, Language: f.Language, Location: &loc}); err != nil {
			return err
		}
		a.references++
		if err := addRelation(Relation{Source: id, Target: parent, Kind: OccursIn, Location: loc,
			Confidence: Exact, Evidence: []Evidence{{Basis: "source_occurrence", Confidence: Exact}}}); err != nil {
			return err
		}
		if decorated >= 0 {
			if err := addRelation(Relation{Source: id, Target: ids[analysis.DeclarationRef(f.Path, decorated)], Kind: Decorates, Location: loc, Confidence: Exact, Evidence: []Evidence{{Basis: "source_modifier", Confidence: Exact}}}); err != nil {
				return err
			}
		}
		if direct != (analysis.Ref{}) {
			target := ids[direct]
			return addRelation(Relation{Source: id, Target: target, Kind: References, Location: loc, Confidence: Exact, Evidence: []Evidence{{Basis: "local_import_binding", Confidence: Exact}}})
		}
		key := referenceKey{ownerRef, kind, f.Path, span.Start, span.End}
		for _, binding := range bindings[key] {
			// Derive both graph views from the same resolver output. This is a
			// projection of evidence, never an independent name-based resolution.
			relation := a.relation(binding)
			relation.ID = ""
			relation.Source, relation.Kind = id, References
			if err := addRelation(relation); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range sortedFiles(files) {
		f := files[path]
		for _, call := range f.Calls {
			if err := addReference(f, Calls, call.Name, call.Receiver, call.Span, analysis.EnclosingDeclaration(f, call.Span), directImport(a.index, f, call.Name, call.Receiver, call.Span, call.Imported), -1); err != nil {
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
			if err := addReference(f, kind, ref.Name, ref.Receiver, ref.Span, ref.Owner, directImport(a.index, f, ref.Name, ref.Receiver, ref.Span, !ref.Bound), decorated); err != nil {
				return err
			}
		}
		for _, hint := range f.TypeRelations {
			if err := addReference(f, RelationKind(hint.Kind), hint.Name, hint.Module, hint.Span, hint.Owner, directImport(a.index, f, hint.Name, hint.Module, hint.Span, !hint.Blocked), -1); err != nil {
				return err
			}
		}
	}
	return nil
}

func directImport(index *analysis.Index, f analysis.Facts, name, receiver string, span analysis.Span, allowed bool) analysis.Ref {
	if allowed && receiver == "" {
		if ref, ok := analysis.LocalImport(f, name, span); ok {
			return ref
		}
		if f.Language == "go" {
			for i := range f.Imports {
				ref := analysis.ImportItemRef(f.Path, i, -1)
				item := index.Entities[ref]
				if item.Binding != nil && item.Binding.Form == "namespace" && item.Binding.LocalName == name {
					return ref
				}
			}
		}
	}
	return analysis.Ref{}
}
