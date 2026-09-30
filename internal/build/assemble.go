package build

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/graphmodel"
	"github.com/compforge/codegraph/internal/pipeline"
)

func (g *Builder) assemble(ctx context.Context, files map[string]analysis.Facts, failures map[string]graphmodel.Diagnostic) (map[string]graphmodel.Node, map[string]graphmodel.Relation, graphmodel.BuildReport, error) {
	nodes := map[string]graphmodel.Node{}
	relations := map[string]graphmodel.Relation{}
	ids := map[analysis.Ref]string{}
	report := graphmodel.BuildReport{Snapshot: g.snapshot, Documents: sortedFiles(files)}
	for _, d := range failures {
		report.Diagnostics = append(report.Diagnostics, d)
		// A failed parser cannot erase the identity of a supplied document.
		// Keep only the Document node; no declarations or relations are inferred.
		if d.Code == "parse_error" {
			id := graphmodel.DocumentID(d.Location.Path)
			nodes[id] = graphmodel.Node{ID: id, Kind: graphmodel.DocumentKind, Name: path.Base(d.Location.Path),
				Language: extract.Language(d.Location.Path), Location: &d.Location}
		}
	}
	index, issues, err := pipeline.BuiltinsWithResolution(ctx, files, g.opts.ModulePath, g.resolution, g.opts.MaxNodes-len(nodes), g.opts.MaxRelations, g.opts.MaxEvidence)
	if err != nil {
		if errors.Is(err, analysis.ErrEvidenceLimit) || errors.Is(err, analysis.ErrEdgeLimit) || errors.Is(err, pipeline.ErrNodeLimit) {
			err = fmt.Errorf("%w: %v", graphmodel.ErrBuildBudget, err)
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
			id = graphmodel.DocumentID(ref.Path)
		case ref.IsDeclaration():
			id = graphmodel.DeclarationID(ref.Path, graphmodel.NodeKind(e.Kind), e.QualifiedName, e.Location.Start)
		default:
			id = "node:" + graphmodel.Identity(ref.SyntheticKey())
		}
		n := graphmodel.Node{ID: id, Kind: graphmodel.NodeKind(e.Kind), Name: e.Name, QualifiedName: e.QualifiedName, Language: e.Language}
		if ref.IsDocument() {
			n.Gitlink = files[ref.Path].Gitlink
		}
		if e.Location != nil {
			f := files[e.Location.Path]
			n.Location = graphmodel.LocationPtr(f, e.Location.Span)
			for _, m := range e.Comments {
				n.Markers = append(n.Markers, graphmodel.Marker{Kind: graphmodel.MarkerKind(m.Kind), Text: m.Text, Location: graphmodel.SourceLocation(f, m.Span)})
			}
		}
		ids[ref], nodes[id] = id, n
	}
	for _, p := range report.Documents {
		f := files[p]
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, graphmodel.ExtractionDiagnostic(f, issue))
		}
	}
	edges := index.Edges
	for _, e := range edges {
		if ids[e.Source] == "" || ids[e.Target] == "" {
			return nil, nil, report, fmt.Errorf("unpublished relation endpoint: %+v", e)
		}
		loc := graphmodel.SourceLocation(files[e.Path], e.Span)
		id := graphmodel.Identity(ids[e.Source], ids[e.Target], e.Kind, loc.Path, loc.StartByte, loc.EndByte)
		r := graphmodel.Relation{ID: id, Source: ids[e.Source], Target: ids[e.Target], Kind: graphmodel.RelationKind(e.Kind), Confidence: graphmodel.Confidence(e.Confidence), Location: loc}
		for _, proof := range e.Evidence {
			evidence := graphmodel.Evidence{Basis: proof.Basis, Confidence: graphmodel.Confidence(proof.Confidence)}
			if proof.Location != nil {
				evidence.Location = graphmodel.LocationPtr(files[proof.Location.Path], proof.Location.Span)
			}
			r.Evidence = append(r.Evidence, evidence)
		}
		relations[id] = r
	}
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, graphmodel.Diagnostic{Code: i.Code, Message: i.Reference,
			Subject: graphmodel.RelationsSubject, Relation: graphmodel.RelationKind(i.Relation), Location: graphmodel.SourceLocation(files[i.Path], i.Span)})
	}
	if len(nodes) > g.opts.MaxNodes || len(relations) > g.opts.MaxRelations {
		return nil, nil, report, fmt.Errorf("%w: nodes=%d relations=%d", graphmodel.ErrBuildBudget, len(nodes), len(relations))
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
