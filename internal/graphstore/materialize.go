package graphstore

import (
	"context"
	"encoding/json"

	"github.com/compforge/codegraph/internal/model"
)

func (g *Snapshot) materialize(ctx context.Context, nodes map[string]model.Node, relations map[string]model.Relation) (*Store, error) {
	s := New(g.limits)
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		props := map[string]any{"id": n.ID, "kind": string(n.Kind), "name": n.Name, "qualifiedName": n.QualifiedName, "language": n.Language, "snapshot": g.snapshot}
		if n.Gitlink != "" {
			props["gitlink"] = n.Gitlink
		}
		if n.Location != nil {
			props["path"], props["line"], props["column"] = n.Location.Path, n.Location.Line, n.Location.Column
			props["startByte"], props["endByte"] = n.Location.StartByte, n.Location.EndByte
		}
		kinds := []string{}
		seen := map[model.MarkerKind]bool{}
		for _, kind := range []model.MarkerKind{model.Spec, model.Case, model.Rule, model.Link, model.Doc} {
			props[string(kind)] = []string{}
		}
		for _, m := range n.Markers {
			if !seen[m.Kind] {
				kinds = append(kinds, string(m.Kind))
				seen[m.Kind] = true
			}
			key := string(m.Kind)
			props[key] = append(props[key].([]string), m.Text)
		}
		props["markers"] = kinds
		encoded, err := json.Marshal(n.Markers)
		if err != nil {
			return nil, err
		}
		props["markerData"] = string(encoded)
		if err := s.AddNode(n.ID, string(n.Kind), props); err != nil {
			return nil, err
		}
	}
	for _, r := range relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		props := map[string]any{"id": r.ID, "kind": string(r.Kind), "source": r.Source, "target": r.Target, "confidence": string(r.Confidence), "bases": evidenceBases(r), "evidenceData": evidenceJSON(r), "path": r.Location.Path, "line": r.Location.Line, "column": r.Location.Column, "startByte": r.Location.StartByte, "endByte": r.Location.EndByte}
		if err := s.AddEdge(r.Source, r.Target, string(r.Kind), props); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// GoGraph properties support scalar lists, not nested maps. Full evidence is
// available on RETURN r and as JSON; bases is a queryable scalar projection.
func evidenceBases(r model.Relation) []string {
	var bases []string
	for _, e := range r.Evidence {
		if len(bases) == 0 || bases[len(bases)-1] != e.Basis {
			bases = append(bases, e.Basis)
		}
	}
	return bases
}
func evidenceJSON(r model.Relation) string { b, _ := json.Marshal(r.Evidence); return string(b) }
