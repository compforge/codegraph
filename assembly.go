package codegraph

import (
	"context"
	"path"

	"github.com/compforge/codegraph/internal/analysis"
)

// graphAssembler owns public object registration and publication budgets.
// +spec=Only selected entities are materialized; every relation has published endpoints.
// +link=component://docs/language-pipeline.md
type graphAssembler struct {
	index                *analysis.Index
	files                map[string]analysis.Facts
	opts                 Options
	tags                 *TagMatcher
	ids                  map[analysis.Ref]string
	nodes                map[string]Node
	relations            map[string]Relation
	evidence, references int
	stage                string
}

func newGraphAssembler(index *analysis.Index, selected selection, opts Options, tags *TagMatcher) *graphAssembler {
	a := &graphAssembler{stage: "publication", index: index, files: index.Files, opts: opts, tags: tags, ids: map[analysis.Ref]string{}, nodes: map[string]Node{}, relations: map[string]Relation{}}
	for ref := range selected {
		e := index.Entities[ref]
		switch {
		case ref.IsDocument():
			a.ids[ref] = DocumentID(ref.Path)
		case ref.IsDeclaration():
			a.ids[ref] = declarationID(ref.Path, NodeKind(e.Kind), e.QualifiedName, e.Location.Start)
		default:
			a.ids[ref] = "node:" + identity(ref.SyntheticKey())
		}
	}
	return a
}
func (a *graphAssembler) addNode(n Node) error {
	if _, exists := a.nodes[n.ID]; !exists && len(a.nodes) >= a.opts.MaxNodes {
		return &BuildBudgetError{Stage: a.stage, Resource: "MaxNodes", Used: len(a.nodes), Adding: 1, Limit: a.opts.MaxNodes}
	}
	a.nodes[n.ID] = n
	return nil
}
func (a *graphAssembler) addRelation(r Relation) error {
	if _, ok := a.nodes[r.Source]; !ok {
		return nil
	}
	if _, ok := a.nodes[r.Target]; !ok {
		return nil
	}
	if r.ID == "" {
		r.ID = identity(r.Source, r.Target, r.Kind, r.Location.Path, r.Location.StartByte, r.Location.EndByte)
	}
	if _, exists := a.relations[r.ID]; exists {
		return nil
	}
	if len(a.relations) >= a.opts.MaxRelations {
		return &BuildBudgetError{Stage: a.stage, Resource: "MaxRelations", Used: len(a.relations), Adding: 1, Limit: a.opts.MaxRelations}
	}
	if len(r.Evidence) > a.opts.MaxEvidence-a.evidence {
		return &BuildBudgetError{Stage: a.stage, Resource: "MaxEvidence", Used: a.evidence, Adding: len(r.Evidence), Limit: a.opts.MaxEvidence}
	}
	a.relations[r.ID] = r
	a.evidence += len(r.Evidence)
	return nil
}
func (a *graphAssembler) relation(e analysis.Edge) Relation {
	loc := location(a.files[e.Path], e.Span)
	r := Relation{ID: identity(a.ids[e.Source], a.ids[e.Target], e.Kind, loc.Path, loc.StartByte, loc.EndByte), Source: a.ids[e.Source], Target: a.ids[e.Target], Kind: RelationKind(e.Kind), Confidence: Confidence(e.Confidence), Location: loc}
	for _, proof := range e.Evidence {
		evidence := Evidence{Basis: proof.Basis, Confidence: Confidence(proof.Confidence)}
		if proof.Location != nil {
			evidence.Location = locationPtr(a.files[proof.Location.Path], proof.Location.Span)
		}
		r.Evidence = append(r.Evidence, evidence)
	}
	return r
}
func (a *graphAssembler) Assemble(ctx context.Context, selected selection, failures map[string]Diagnostic) error {
	for ref := range selected {
		e := a.index.Entities[ref]
		if err := ctx.Err(); err != nil {
			return err
		}
		id := a.ids[ref]
		n := Node{ID: id, Kind: NodeKind(e.Kind), Entrypoint: e.Entrypoint, Name: e.Name, QualifiedName: e.QualifiedName, Language: e.Language}
		if e.Binding != nil {
			b := ModuleBinding(*e.Binding)
			n.Binding = &b
		}
		if ref.IsDocument() {
			n.Gitlink = a.files[ref.Path].Gitlink
			if n.Language == "" && n.Gitlink == "" {
				n.Language = Language(ref.Path)
			}
			n.Manifest = projectManifest(a.files[ref.Path])
		}
		if e.Location != nil {
			f := a.files[e.Location.Path]
			n.Location = locationPtr(f, e.Location.Span)
			n.Documentation = projectDocumentation(f, e.Documentation)
			for _, m := range e.Comments {
				n.Markers = append(n.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
			}
		}
		if e.SignatureLocation != nil {
			f := a.files[e.SignatureLocation.Path]
			span := e.SignatureLocation.Span
			n.Signature = string(f.Source[span.Start:span.End])
			n.SignatureLocation = locationPtr(f, span)
		}
		if e.NameLocation != nil {
			n.NameLocation = locationPtr(a.files[e.NameLocation.Path], e.NameLocation.Span)
		}
		if err := a.addNode(n); err != nil {
			return err
		}
	}

	for _, d := range failures {
		if d.Code == "parse_error" && permits(a.opts.Admission, DocumentNodeKind) {
			loc := d.Location
			if err := a.addNode(Node{ID: DocumentID(loc.Path), Kind: DocumentNodeKind, Name: path.Base(loc.Path), Language: Language(loc.Path), Location: &loc}); err != nil {
				return err
			}
		}
	}
	for _, e := range a.index.Edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Decorator occurrences own the public decorates relation, as in the full graph.
		if e.Kind == string(Decorates) {
			continue
		}
		if _, ok := selected[e.Source]; !ok {
			continue
		}
		if _, ok := selected[e.Target]; !ok {
			continue
		}
		if err := a.addRelation(a.relation(e)); err != nil {
			return err
		}
	}
	if err := a.publishDocumentStructure(ctx, failures); err != nil {
		return err
	}
	if permits(a.opts.Admission, Reference) {
		return a.publishReferences(ctx)
	}
	return nil
}
