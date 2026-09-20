package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/graphstore"
	"github.com/compforge/codegraph/internal/resolve"
)

func FileID(name string) string { return "file:" + name }

func identity(parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func location(f extract.Facts, span extract.Span) Location {
	line := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.Start })
	endLine := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.End })
	if endLine == 0 {
		endLine = 1
	}
	return Location{Path: f.Path, StartByte: span.Start, EndByte: span.End, Line: line, Column: span.Start - f.LineStarts[line-1] + 1, EndLine: endLine, EndColumn: span.End - f.LineStarts[endLine-1] + 1}
}

func (g *Graph) assemble(ctx context.Context, files map[string]extract.Facts, failures map[string]Diagnostic) (map[string]Node, map[string]Relation, BuildReport, error) {
	nodes := map[string]Node{}
	relations := map[string]Relation{}
	ids := map[resolve.Ref]string{}
	report := BuildReport{Snapshot: g.snapshot, Files: sortedFiles(files)}
	for _, d := range failures {
		report.Diagnostics = append(report.Diagnostics, d)
	}
	addEdge := func(source, target string, kind RelationKind, confidence Confidence, basis string, loc Location) {
		id := identity(source, target, kind, loc.Path, loc.StartByte, loc.EndByte)
		relations[id] = Relation{ID: id, Source: source, Target: target, Kind: kind, Confidence: confidence, Basis: basis, Location: loc}
	}
	for _, p := range report.Files {
		if err := ctx.Err(); err != nil {
			return nil, nil, report, err
		}
		f := files[p]
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: issue.Code, Message: issue.Message, Location: location(f, issue.Span)})
		}
		if len(nodes)+1+len(f.Declarations) > g.opts.MaxNodes || len(relations)+len(f.Declarations) > g.opts.MaxRelations {
			return nil, nil, report, fmt.Errorf("%w: declaration graph size", ErrBuildBudget)
		}
		fid := FileID(p)
		ids[resolve.Ref{Path: p, Declaration: -1}] = fid
		nodes[fid] = Node{ID: fid, Kind: File, Name: path.Base(p), Language: f.Language, Location: location(f, extract.Span{Start: 0, End: len(f.Source)})}
		for i, d := range f.Declarations {
			kind, err := declarationKind(d.Kind)
			if err != nil {
				return nil, nil, report, fmt.Errorf("%s: %w", p, err)
			}
			id := "node:" + identity(p, kind, d.QualifiedName, d.Start)
			ids[resolve.Ref{Path: p, Declaration: i}] = id
			n := Node{ID: id, Kind: kind, Name: d.Name, QualifiedName: d.QualifiedName, Language: f.Language, Location: location(f, d.Span)}
			for _, m := range d.Comments {
				n.Markers = append(n.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
			}
			nodes[id] = n
		}
		for i, d := range f.Declarations {
			parent := ids[resolve.Ref{Path: p, Declaration: d.Parent}]
			addEdge(parent, ids[resolve.Ref{Path: p, Declaration: i}], Contains, Exact, "declaration", location(f, d.Span))
		}
	}
	edges, issues, err := resolve.Resolve(ctx, files, g.opts.ModulePath, g.opts.MaxRelations-len(relations))
	if err != nil {
		if errors.Is(err, resolve.ErrEdgeLimit) {
			err = fmt.Errorf("%w: %v", ErrBuildBudget, err)
		}
		return nil, nil, report, err
	}
	for _, e := range edges {
		addEdge(ids[e.Source], ids[e.Target], RelationKind(e.Kind), Confidence(e.Confidence), e.Basis, location(files[e.Path], e.Span))
	}
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: i.Code, Message: i.Reference, Location: location(files[i.Path], i.Span)})
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
	report.Complete = len(report.Diagnostics) == 0
	return nodes, relations, report, nil
}

func (g *Graph) materialize(ctx context.Context, nodes map[string]Node, relations map[string]Relation) (*graphstore.Store, error) {
	s := graphstore.New(g.limits())
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		props := map[string]any{"id": n.ID, "kind": string(n.Kind), "name": n.Name, "qualifiedName": n.QualifiedName, "language": n.Language, "path": n.Location.Path, "line": n.Location.Line, "column": n.Location.Column, "startByte": n.Location.StartByte, "endByte": n.Location.EndByte, "snapshot": g.snapshot}
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
		props := map[string]any{"id": r.ID, "kind": string(r.Kind), "source": r.Source, "target": r.Target, "confidence": string(r.Confidence), "basis": r.Basis, "path": r.Location.Path, "line": r.Location.Line, "column": r.Location.Column, "startByte": r.Location.StartByte, "endByte": r.Location.EndByte}
		if err := s.AddEdge(r.Source, r.Target, string(r.Kind), props); err != nil {
			return nil, err
		}
	}
	return s, nil
}
