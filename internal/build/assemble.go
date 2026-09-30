package build

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/model"
	"github.com/compforge/codegraph/internal/pipeline"
)

func (g *Builder) assemble(ctx context.Context, files map[string]analysis.Facts, failures map[string]model.Diagnostic) (map[string]model.Node, map[string]model.Relation, model.BuildReport, error) {
	nodes := map[string]model.Node{}
	relations := map[string]model.Relation{}
	ids := map[analysis.Ref]string{}
	report := model.BuildReport{Snapshot: g.snapshot, Documents: sortedFiles(files)}
	for _, d := range failures {
		report.Diagnostics = append(report.Diagnostics, d)
		// A failed parser cannot erase the identity of a supplied document.
		// Keep only the Document node; no declarations or relations are inferred.
		if d.Code == "parse_error" {
			id := model.DocumentID(d.Location.Path)
			nodes[id] = model.Node{ID: id, Kind: model.DocumentKind, Name: path.Base(d.Location.Path),
				Language: extract.Language(d.Location.Path), Location: &d.Location}
		}
	}
	index, issues, err := pipeline.BuiltinsWithResolution(ctx, files, g.opts.ModulePath, g.resolution, g.opts.MaxNodes-len(nodes), g.opts.MaxRelations, g.opts.MaxEvidence)
	if err != nil {
		if errors.Is(err, analysis.ErrEvidenceLimit) || errors.Is(err, analysis.ErrEdgeLimit) || errors.Is(err, pipeline.ErrNodeLimit) {
			err = fmt.Errorf("%w: %v", model.ErrBuildBudget, err)
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
			id = model.DocumentID(ref.Path)
		case ref.IsDeclaration():
			id = model.DeclarationID(ref.Path, model.NodeKind(e.Kind), e.QualifiedName, e.Location.Start)
		default:
			id = "node:" + model.Identity(ref.SyntheticKey())
		}
		n := model.Node{ID: id, Kind: model.NodeKind(e.Kind), Name: e.Name, QualifiedName: e.QualifiedName, Language: e.Language}
		if ref.IsDocument() {
			n.Gitlink = files[ref.Path].Gitlink
		}
		if e.Location != nil {
			f := files[e.Location.Path]
			n.Location = model.LocationPtr(f, e.Location.Span)
			for _, m := range e.Comments {
				n.Markers = append(n.Markers, model.Marker{Kind: model.MarkerKind(m.Kind), Text: m.Text, Location: model.SourceLocation(f, m.Span)})
			}
		}
		ids[ref], nodes[id] = id, n
	}
	for _, p := range report.Documents {
		f := files[p]
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, model.ExtractionDiagnostic(f, issue))
		}
	}
	edges := index.Edges
	for _, e := range edges {
		if ids[e.Source] == "" || ids[e.Target] == "" {
			return nil, nil, report, fmt.Errorf("unpublished relation endpoint: %+v", e)
		}
		loc := model.SourceLocation(files[e.Path], e.Span)
		id := model.Identity(ids[e.Source], ids[e.Target], e.Kind, loc.Path, loc.StartByte, loc.EndByte)
		r := model.Relation{ID: id, Source: ids[e.Source], Target: ids[e.Target], Kind: model.RelationKind(e.Kind), Confidence: model.Confidence(e.Confidence), Location: loc}
		for _, proof := range e.Evidence {
			evidence := model.Evidence{Basis: proof.Basis, Confidence: model.Confidence(proof.Confidence)}
			if proof.Location != nil {
				evidence.Location = model.LocationPtr(files[proof.Location.Path], proof.Location.Span)
			}
			r.Evidence = append(r.Evidence, evidence)
		}
		relations[id] = r
	}
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, model.Diagnostic{Code: i.Code, Message: i.Reference,
			Subject: model.RelationsSubject, Relation: model.RelationKind(i.Relation), Location: model.SourceLocation(files[i.Path], i.Span)})
	}
	if len(nodes) > g.opts.MaxNodes || len(relations) > g.opts.MaxRelations {
		return nil, nil, report, fmt.Errorf("%w: nodes=%d relations=%d", model.ErrBuildBudget, len(nodes), len(relations))
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
