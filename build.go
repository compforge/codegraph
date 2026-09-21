package codegraph

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

type Diagnostic struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
}

// Complete means every supplied document and extracted reference in this
// scope was handled. It never claims whole-repository or compiler completeness.
type BuildReport struct {
	Snapshot         string       `json:"snapshot"`
	Files            []string     `json:"files"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
	Complete         bool         `json:"complete"`
	Nodes, Relations int
}

func cloneReport(r BuildReport) BuildReport {
	r.Files = append([]string(nil), r.Files...)
	r.Diagnostics = append([]Diagnostic(nil), r.Diagnostics...)
	return r
}

// Build creates a graph from one explicit source-document batch. Document
// selection and source loading belong to the caller; this entrypoint never
// discovers additional files implicitly.
func Build(ctx context.Context, snapshot string, documents []Document, opts Options) (*Graph, BuildReport, error) {
	g, err := New(snapshot, opts)
	if err != nil {
		return nil, BuildReport{}, err
	}
	r, err := g.AddDocuments(ctx, documents...)
	if err != nil {
		return nil, r, err
	}
	return g, r, nil
}

// add extracts and publishes one explicit document batch atomically.
func (g *Graph) add(ctx context.Context, documents ...Document) (BuildReport, error) {
	g.buildMu.Lock()
	defer g.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	for _, document := range documents {
		if !fs.ValidPath(document.Path) {
			return g.Report(), fmt.Errorf("invalid source path %q", document.Path)
		}
	}
	staged := make(map[string]extract.Facts, len(g.files))
	var total int64
	for p, f := range g.files {
		staged[p] = f
		total += int64(len(f.Source))
	}
	failures := map[string]Diagnostic{}
	for p, d := range g.failures {
		failures[p] = d
	}
	if err := g.stageDocuments(ctx, documents, staged, failures, total); err != nil {
		return g.Report(), err
	}
	nodes, relations, report, err := g.assemble(ctx, staged, failures)
	if err != nil {
		return g.Report(), err
	}
	store, err := g.materialize(ctx, nodes, relations)
	if err != nil {
		return g.Report(), err
	}
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	// Publish only after the complete batch, including all edge properties, exists.
	g.mu.Lock()
	defer g.mu.Unlock()
	g.files, g.failures, g.nodes, g.relations, g.store, g.report = staged, failures, nodes, relations, store, report
	return cloneReport(report), nil
}

func (g *Graph) allowed(p string) bool {
	for _, s := range g.opts.Scope {
		if s == "." || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return len(g.opts.Scope) == 0
}
func (g *Graph) allowedDir(p string) bool { return g.allowed(p) }

func sortedFiles(files map[string]extract.Facts) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
