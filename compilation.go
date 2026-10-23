package codegraph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/compforge/codegraph/internal/pipeline"
)

// compilation owns one build attempt; only Builder may publish its completed result.
// +spec=Failure or cancellation discards this attempt without changing the last Graph.
// +link=component://docs/language-pipeline.md
type compilation struct {
	snapshot    string
	opts        Options
	resolution  analysis.ResolutionContext
	tags        *TagMatcher
	files       map[string]analysis.Facts
	failures    map[string]Diagnostic
	diagnostics []Diagnostic
}

func (c *compilation) Run(ctx context.Context) (map[string]Node, map[string]Relation, BuildReport, error) {
	report := BuildReport{Snapshot: c.snapshot, Documents: sortedFiles(c.files), Diagnostics: c.diagnostics}
	report.Materials.Submitted = len(c.files) + len(c.failures) + len(c.diagnostics)
	report.Materials.Accepted = len(c.files)
	report.Materials.Excluded = len(c.diagnostics)
	failures := map[string]Diagnostic{}
	for name, d := range c.failures {
		if p := c.opts.Admission; p != nil {
			if tag := c.tags.ExcludedBy(Document{Path: name}, p.ExcludedTags); tag != "" {
				report.Materials.Excluded++
				d.Code = "excluded_material"
				d.Message = name + " excluded by " + string(tag) + " material or ancestor tag"
				report.Diagnostics = append(report.Diagnostics, d)
				continue
			}
		}
		if d.Code == "parse_error" {
			report.Materials.Failed++
		}
		failures[name] = d
		report.Diagnostics = append(report.Diagnostics, d)
	}
	for _, p := range report.Documents {
		f := c.files[p]
		uses := len(f.Calls) + len(f.References) + len(f.TypeRelations)
		if uses > c.opts.Analysis.Uses-report.Analysis.Uses {
			return nil, nil, report, &BuildBudgetError{Stage: "analysis", Resource: "Uses", Used: report.Analysis.Uses, Adding: uses, Limit: c.opts.Analysis.Uses}
		}
		report.Analysis.Uses += uses
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, extractionDiagnostic(f, issue))
		}
	}
	scope := analysis.NewBuildScope(c.files, c.opts.ModulePath)
	scope.Resolution = c.resolution
	analyzer := pipeline.Analyzer{Limits: analysis.Limits{Entities: c.opts.Analysis.Entities, Relations: c.opts.Analysis.Relations, Evidence: c.opts.Analysis.Evidence}, Lookup: language.Lookup}
	started := time.Now()
	index, issues, err := analyzer.Analyze(ctx, scope)
	report.Analysis.Duration = time.Since(started)
	if err != nil {
		var limit *analysis.LimitError
		if errors.As(err, &limit) {
			err = &BuildBudgetError{Stage: "analysis", Resource: limit.Resource, Used: limit.Used, Adding: limit.Adding, Limit: limit.Limit}
		}
		if errors.Is(err, analysis.ErrEvidenceLimit) {
			err = &BuildBudgetError{Stage: "analysis", Resource: "Evidence", Used: c.opts.Analysis.Evidence, Adding: 1, Limit: c.opts.Analysis.Evidence}
		}
		return nil, nil, report, err
	}
	for _, edge := range index.Edges {
		if err := ctx.Err(); err != nil {
			return nil, nil, report, err
		}
		_, source := index.Entities[edge.Source]
		_, target := index.Entities[edge.Target]
		if !source || !target {
			return nil, nil, report, fmt.Errorf("analysis relation has unregistered endpoint: kind=%s path=%s start=%d", edge.Kind, edge.Path, edge.Span.Start)
		}
	}
	report.Analysis.Entities = len(index.Entities)
	report.Analysis.Relations = len(index.Edges)
	report.Analysis.Evidence = index.EvidenceCount
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: i.Code, Message: i.Reference, Subject: RelationsSubject, Relation: RelationKind(i.Relation), Location: location(c.files[i.Path], i.Span)})
	}
	selector := entitySelector{index: index, policy: c.opts.Admission, limit: c.opts.Analysis.SelectionSteps}
	started = time.Now()
	selected, err := selector.Select(ctx)
	report.Selection.Duration = time.Since(started)
	report.Selection.Steps = selector.steps
	if err != nil {
		return nil, nil, report, err
	}
	for _, reason := range selected {
		switch reason {
		case coreSelection:
			report.Selection.Core++
		case bridgeSelection:
			report.Selection.Bridges++
		case contextSelection:
			report.Selection.Context++
		}
	}
	started = time.Now()
	assembler := newGraphAssembler(index, selected, c.opts, c.tags)
	if err := assembler.Assemble(ctx, selected, failures); err != nil {
		return nil, nil, report, err
	}
	report.Publication.Duration = time.Since(started)
	report.Publication.References = assembler.references
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
	report.Nodes, report.Relations = len(assembler.nodes), len(assembler.relations)
	return assembler.nodes, assembler.relations, report, nil
}

func (b *Builder) assemble(ctx context.Context, files map[string]analysis.Facts, failures map[string]Diagnostic) (map[string]Node, map[string]Relation, BuildReport, error) {
	files, excluded := b.admittedFiles(files)
	work := compilation{snapshot: b.snapshot, opts: b.opts, resolution: b.resolution, tags: b.tagMatcher, files: files, failures: failures, diagnostics: excluded}
	return work.Run(ctx)
}
