package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
	"github.com/compforge/codegraph/internal/resolve"
)

type Diagnostic struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
}

// Complete means every requested/expanded file and extracted reference in this
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

func Build(ctx context.Context, snapshot string, source fs.FS, files []string, opts Options) (*Graph, BuildReport, error) {
	g, err := New(snapshot, opts)
	if err != nil {
		return nil, BuildReport{}, err
	}
	r, err := g.AddFiles(ctx, source, files...)
	if err != nil {
		return nil, r, err
	}
	return g, r, nil
}

// AddFiles adds facts from source to this snapshot. Callers must supply the same
// immutable filesystem revision across calls. Re-adding different bytes at an
// existing path fails; add new versions to a different Graph.
// Read/parse/unsupported-file failures are reported as partial coverage. Budget,
// identity and cancellation failures roll back the batch and return an error.
func (g *Graph) AddFiles(ctx context.Context, source fs.FS, files ...string) (BuildReport, error) {
	g.buildMu.Lock()
	defer g.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return g.Report(), err
	}
	if source == nil {
		return g.Report(), fmt.Errorf("source filesystem is nil")
	}
	for _, p := range files {
		if !fs.ValidPath(p) {
			return g.Report(), fmt.Errorf("invalid source path %q", p)
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
	type entry struct {
		file  string
		depth int
	}
	queue := []entry{}
	for _, p := range files {
		queue = append(queue, entry{p, 0})
	}
	seen := map[string]int{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return g.Report(), err
		}
		item := queue[0]
		queue = queue[1:]
		if depth, ok := seen[item.file]; ok && depth <= item.depth {
			continue
		}
		seen[item.file] = item.depth
		issue := func(code, msg string) {
			failures[item.file] = Diagnostic{Code: code, Message: msg, Location: Location{Path: item.file}}
		}
		if !g.allowed(item.file) {
			issue("out_of_scope", "file is outside allowed scope")
			continue
		}
		if item.depth > g.opts.MaxDepth {
			return g.Report(), fmt.Errorf("%w: import expansion depth reached at %s", ErrBuildBudget, item.file)
		}
		if extract.Detect(item.file) == nil {
			issue("unsupported_language", "no registered grammar for file")
			continue
		}
		old, exists := staged[item.file]
		if !exists && len(staged) >= g.opts.MaxFiles {
			return g.Report(), fmt.Errorf("%w: file limit %d", ErrBuildBudget, g.opts.MaxFiles)
		}
		fd, err := source.Open(item.file)
		if err != nil {
			issue("read_error", err.Error())
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(fd, g.opts.MaxFileBytes+1))
		closeErr := fd.Close()
		if readErr != nil {
			issue("read_error", readErr.Error())
			continue
		}
		if closeErr != nil {
			issue("read_error", closeErr.Error())
			continue
		}
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
			f, err := extract.Analyze(ctx, item.file, data, g.opts.ParseTimeout)
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
		if !g.opts.ExpandImports {
			continue
		}
		f := staged[item.file]
		if f.Language != "go" {
			for _, imp := range f.Imports {
				for _, candidate := range resolve.ImportPaths(f, imp) {
					if !g.allowed(candidate) {
						continue
					}
					info, err := fs.Stat(source, candidate)
					if err == nil && !info.IsDir() {
						queue = append(queue, entry{candidate, item.depth + 1})
					}
				}
			}
			continue
		}
		dirs := []entry{{path.Dir(item.file), item.depth}}
		for _, i := range f.Imports {
			if dir, ok := resolve.ImportDir(g.opts.ModulePath, i.Path); ok && fs.ValidPath(dir) {
				dirs = append(dirs, entry{dir, item.depth + 1})
			}
		}
		for _, dir := range dirs {
			if !g.allowedDir(dir.file) {
				continue
			}
			entries, err := fs.ReadDir(source, dir.file)
			if err != nil {
				issue("read_directory", err.Error())
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
					if strings.HasSuffix(e.Name(), "_test.go") && !strings.HasSuffix(item.file, "_test.go") {
						continue
					}
					queue = append(queue, entry{path.Join(dir.file, e.Name()), dir.depth})
				}
			}
		}
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
