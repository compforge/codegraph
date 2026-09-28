package analysis

import "sort"

type edgeKey struct {
	Source, Target Ref
	Kind, Path     string
	Span           Span
}
type proofKey struct {
	Basis      string
	Confidence Confidence
	Location   SourceLocation
	Located    bool
}

func proofIdentity(e Evidence) proofKey {
	k := proofKey{Basis: e.Basis, Confidence: e.Confidence}
	if e.Location != nil {
		k.Location, k.Located = *e.Location, true
	}
	return k
}
func (x *Index) registerEdge(e Edge) bool {
	key := edgeKey{e.Source, e.Target, e.Kind, e.Path, e.Span}
	i, exists := x.edgeIDs[key]
	if !exists {
		i = len(x.Edges)
		x.edgeIDs[key] = i
		c := e
		c.Evidence = nil
		x.Edges = append(x.Edges, c)
	}
	proofs := e.Evidence
	if len(proofs) == 0 {
		proofs = []Evidence{{Basis: e.Basis, Confidence: e.Confidence}}
	}
	r := &x.Edges[i]
	for _, proof := range proofs {
		duplicate := false
		for _, old := range r.Evidence {
			if proofIdentity(old) == proofIdentity(proof) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if proof.Location != nil {
			loc := *proof.Location
			proof.Location = &loc
		}
		r.Evidence = append(r.Evidence, proof)
		x.EvidenceCount++
	}
	sort.Slice(r.Evidence, func(i, j int) bool {
		a, b := proofIdentity(r.Evidence[i]), proofIdentity(r.Evidence[j])
		if a.Basis != b.Basis {
			return a.Basis < b.Basis
		}
		if a.Confidence != b.Confidence {
			return b.Confidence.AtLeast(a.Confidence)
		}
		if a.Located != b.Located {
			return !a.Located
		}
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.Start != b.Location.Start {
			return a.Location.Start < b.Location.Start
		}
		return a.Location.End < b.Location.End
	})
	r.Confidence = ""
	for _, proof := range r.Evidence {
		r.Confidence = r.Confidence.Stronger(proof.Confidence)
	}
	r.Basis = "" // Aggregate relations have no single privileged derivation.
	return !exists
}

// Conflicts retain contradictory unique-target claims at their source site.
// Lower-tier alternatives and organization membership are deliberately excluded.
func (x *Index) Conflicts() []Gap {
	type site struct {
		Source     Ref
		Kind, Path string
		Span       Span
	}
	seen := map[site]Ref{}
	reported := map[site]bool{}
	var gaps []Gap
	for _, e := range x.Edges {
		if e.Confidence != "exact" || (e.Kind != "calls" && e.Kind != "references") {
			continue
		}
		k := site{e.Source, e.Kind, e.Path, e.Span}
		if target, ok := seen[k]; ok && target != e.Target && !reported[k] {
			gaps = append(gaps, Gap{Path: e.Path, Span: e.Span, Code: "conflicting_binding", Reference: "multiple exact targets for one occurrence", Relation: e.Kind})
			reported[k] = true
		}
		seen[k] = e.Target
	}
	return gaps
}
