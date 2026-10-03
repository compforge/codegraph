package codegraph

import (
	"context"
	"fmt"

	"github.com/compforge/codegraph/internal/analysis"
)

// A binding is located at a source use, even when other uses share its name.
// Owner participates in the key so a resolver cannot silently reparent a use.
type occurrenceKey struct {
	owner      string
	kind       RelationKind
	path       string
	start, end int
}

// publishOccurrences is the production-side handoff from extraction to graph
// facts. Uses exist before target binding; they never create external symbols.
// +spec=Every extracted call/reference has a source-use node even without a target; candidate targets retain the resolver's evidence.
// +why=Consumers must be able to inspect a partial graph without reading Facts or treating diagnostics as code facts.
func publishOccurrences(ctx context.Context, files map[string]analysis.Facts, ids map[analysis.Ref]string, nodes map[string]Node, relations map[string]Relation, opts Options) error {
	bindings := make(map[occurrenceKey][]Relation)
	evidenceCount := 0
	for _, r := range relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		evidenceCount += len(r.Evidence)
		if r.Kind == Calls || r.Kind == References {
			key := occurrenceKey{r.Source, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte}
			bindings[key] = append(bindings[key], r)
		}
	}
	addRelation := func(r Relation) error {
		r.ID = identity(r.Source, r.Target, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte)
		if _, exists := relations[r.ID]; exists {
			return nil
		}
		if len(relations) >= opts.MaxRelations || evidenceCount+len(r.Evidence) > opts.MaxEvidence {
			return fmt.Errorf("%w: source-use relations or evidence", ErrBuildBudget)
		}
		relations[r.ID] = r
		evidenceCount += len(r.Evidence)
		return nil
	}
	addUse := func(f analysis.Facts, kind NodeKind, relation RelationKind, name, receiver string, span analysis.Span, owner int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Source identity depends on this occurrence, never on available targets
		// or precision, so supplementing documents does not rename existing uses.
		id := "node:" + identity(f.Path, kind, span.Start, span.End, name, receiver)
		if _, exists := nodes[id]; exists {
			return nil
		}
		if len(nodes) >= opts.MaxNodes {
			return fmt.Errorf("%w: source-use nodes", ErrBuildBudget)
		}
		parent := ids[analysis.SourceRef(f.Path, owner)]
		if parent == "" {
			return fmt.Errorf("unpublished source-use owner: %s at %d", f.Path, span.Start)
		}
		loc := location(f, span)
		nodes[id] = Node{ID: id, Kind: kind, Name: name, Receiver: receiver, Language: f.Language, Location: &loc}
		if err := addRelation(Relation{Source: id, Target: parent, Kind: OccursIn, Location: loc,
			Confidence: Exact, Evidence: []Evidence{{Basis: "source_occurrence", Confidence: Exact}}}); err != nil {
			return err
		}
		key := occurrenceKey{parent, relation, f.Path, span.Start, span.End}
		for _, binding := range bindings[key] {
			// Derive both graph views from the same resolver output. This is a
			// projection of evidence, never an independent name-based resolution.
			binding = cloneRelation(binding)
			binding.Source, binding.Kind = id, ResolvesTo
			if err := addRelation(binding); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range sortedFiles(files) {
		f := files[path]
		for _, call := range f.Calls {
			if err := addUse(f, CallSite, Calls, call.Name, call.Receiver, call.Span, analysis.EnclosingDeclaration(f, call.Span)); err != nil {
				return err
			}
		}
		for _, ref := range f.References {
			if err := addUse(f, ReferenceSite, References, ref.Name, ref.Receiver, ref.Span, ref.Owner); err != nil {
				return err
			}
		}
	}
	return nil
}
