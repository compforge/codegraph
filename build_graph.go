package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/graphstore"
	"github.com/compforge/codegraph/internal/pipeline"
)

// DocumentID identifies a document node by its snapshot-relative logical path.
func DocumentID(name string) string { return "document:" + name }

// declarationID is shared by early detached results and published nodes.
func declarationID(path string, kind NodeKind, qualifiedName string, start int) string {
	return "node:" + identity(path, kind, qualifiedName, start)
}

func identity(parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func location(f analysis.Facts, span analysis.Span) Location {
	line := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.Start })
	endLine := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.End })
	if endLine == 0 {
		endLine = 1
	}
	return Location{Path: f.Path, StartByte: span.Start, EndByte: span.End, Line: line, Column: span.Start - f.LineStarts[line-1] + 1, EndLine: endLine, EndColumn: span.End - f.LineStarts[endLine-1] + 1}
}

func (g *Graph) assemble(ctx context.Context, files map[string]analysis.Facts, failures map[string]Diagnostic) (map[string]Node, map[string]Relation, BuildReport, error) {
	nodes := map[string]Node{}
	relations := map[string]Relation{}
	ids := map[analysis.Ref]string{}
	report := BuildReport{Snapshot: g.snapshot, Documents: sortedFiles(files)}
	for _, d := range failures {
		report.Diagnostics = append(report.Diagnostics, d)
		// A failed parser cannot erase the identity of a supplied document.
		// Keep only the Document node; no declarations or relations are inferred.
		if d.Code == "parse_error" {
			id := DocumentID(d.Location.Path)
			nodes[id] = Node{ID: id, Kind: DocumentKind, Name: path.Base(d.Location.Path),
				Language: Language(d.Location.Path), Location: &d.Location}
		}
	}
	index, issues, err := pipeline.Builtins(ctx, files, g.opts.ModulePath, g.opts.MaxNodes-len(nodes), g.opts.MaxRelations, g.opts.MaxEvidence)
	if err != nil {
		if errors.Is(err, analysis.ErrEvidenceLimit) || errors.Is(err, analysis.ErrEdgeLimit) || errors.Is(err, pipeline.ErrNodeLimit) {
			err = fmt.Errorf("%w: %v", ErrBuildBudget, err)
		}
		return nil, nil, report, err
	}

	for ref, e := range index.Entities {
		if err := ctx.Err(); err != nil {
			return nil, nil, report, err
		}
		var id string
		switch {
		case ref.IsDocument():
			id = DocumentID(ref.Path)
		case ref.IsDeclaration():
			id = declarationID(ref.Path, NodeKind(e.Kind), e.QualifiedName, e.Location.Start)
		default:
			id = "node:" + identity(ref.SyntheticKey())
		}
		n := Node{ID: id, Kind: NodeKind(e.Kind), Name: e.Name, QualifiedName: e.QualifiedName, Language: e.Language}
		if ref.IsDocument() {
			n.Gitlink = files[ref.Path].Gitlink
		}
		if e.Location != nil {
			f := files[e.Location.Path]
			n.Location = locationPtr(f, e.Location.Span)
			for _, m := range e.Comments {
				n.Markers = append(n.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
			}
		}
		ids[ref], nodes[id] = id, n
	}
	for _, p := range report.Documents {
		f := files[p]
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, extractionDiagnostic(f, issue))
		}
	}
	edges := index.Edges
	for _, e := range edges {
		if ids[e.Source] == "" || ids[e.Target] == "" {
			return nil, nil, report, fmt.Errorf("unpublished relation endpoint: %+v", e)
		}
		loc := location(files[e.Path], e.Span)
		id := identity(ids[e.Source], ids[e.Target], e.Kind, loc.Path, loc.StartByte, loc.EndByte)
		r := Relation{ID: id, Source: ids[e.Source], Target: ids[e.Target], Kind: RelationKind(e.Kind), Confidence: Confidence(e.Confidence), Location: loc}
		for _, proof := range e.Evidence {
			evidence := Evidence{Basis: proof.Basis, Confidence: Confidence(proof.Confidence)}
			if proof.Location != nil {
				evidence.Location = locationPtr(files[proof.Location.Path], proof.Location.Span)
			}
			r.Evidence = append(r.Evidence, evidence)
		}
		relations[id] = r
	}
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: i.Code, Message: i.Reference,
			Subject: RelationsSubject, Relation: RelationKind(i.Relation), Location: location(files[i.Path], i.Span)})
	}
	if len(nodes) > g.opts.MaxNodes || len(relations) > g.opts.MaxRelations {
		return nil, nil, report, fmt.Errorf("%w: nodes=%d relations=%d", ErrBuildBudget, len(nodes), len(relations))
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.StartByte != b.Location.StartByte {
			return a.Location.StartByte < b.Location.StartByte
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	report.Nodes, report.Relations = len(nodes), len(relations)
	return nodes, relations, report, nil
}

func (g *Graph) materialize(ctx context.Context, nodes map[string]Node, relations map[string]Relation) (*graphstore.Store, error) {
	s := graphstore.New(g.limits())
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
		seen := map[MarkerKind]bool{}
		for _, kind := range []MarkerKind{Spec, Case, Rule, Link, Doc} {
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

func locationPtr(f analysis.Facts, span analysis.Span) *Location {
	loc := location(f, span)
	return &loc
}

// GoGraph properties support scalar lists, not nested maps. Full evidence is
// available on RETURN r and as JSON; bases is a queryable scalar projection.
func evidenceBases(r Relation) []string {
	var bases []string
	for _, e := range r.Evidence {
		if len(bases) == 0 || bases[len(bases)-1] != e.Basis {
			bases = append(bases, e.Basis)
		}
	}
	return bases
}
func evidenceJSON(r Relation) string { b, _ := json.Marshal(r.Evidence); return string(b) }
