package codegraph

import (
	"bytes"
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
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return g.Report(), err
		}
		item := struct{ file string }{document.Path}
		issue := func(code, msg string) {
			failures[item.file] = Diagnostic{Code: code, Message: msg, Location: Location{Path: item.file}}
		}
		if !g.allowed(item.file) {
			issue("out_of_scope", "file is outside allowed scope")
			continue
		}
		if extract.Detect(item.file) == nil {
			issue("unsupported_language", "no registered grammar for file")
			continue
		}
		old, exists := staged[item.file]
		if !exists && len(staged) >= g.opts.MaxFiles {
			return g.Report(), fmt.Errorf("%w: file limit %d", ErrBuildBudget, g.opts.MaxFiles)
		}
		data := document.Content
		if int64(len(data)) > g.opts.MaxFileBytes {
			return g.Report(), fmt.Errorf("%w: file %s exceeds byte limit", ErrBuildBudget, item.file)
		}
		if exists && !bytes.Equal(data, old.Source) {
			return g.Report(), fmt.Errorf("%w: %s", ErrSnapshotChanged, item.file)
		}
		if !exists {
			if total+int64(len(data)) > g.opts.MaxSourceBytes {
				return g.Report(), fmt.Errorf("%w: source byte limit", ErrBuildBudget)
			}
			// Facts retain source bytes for subsequent batches. Detach caller-owned
			// document content before handing it to extraction.
			f, err := extract.Analyze(ctx, document.Path, bytes.Clone(data), g.opts.ParseTimeout)
			if err != nil {
				if ctx.Err() != nil {
					return g.Report(), ctx.Err()
				}
				issue("parse_error", err.Error())
				continue
			}
			staged[item.file] = f
			total += int64(len(data))
		}
		delete(failures, item.file)
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
