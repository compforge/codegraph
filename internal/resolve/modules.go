package resolve

import (
	"context"
	"io/fs"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

// ImportPaths is shared by expansion and resolution. It proposes only bounded
// repository-relative paths, never walks a repository or installs dependencies.
func ImportPaths(f extract.Facts, imp extract.Import) []string {
	var paths []string
	if f.Language == "python" {
		module := imp.Path
		if imp.From != "" {
			module = imp.From
		}
		base := "."
		if imp.Relative > 0 {
			base = path.Dir(f.Path)
			for i := 1; i < imp.Relative; i++ {
				if base == "." {
					return nil
				}
				base = path.Dir(base)
			}
		}
		module = strings.TrimLeft(module, ".")
		name := path.Join(base, strings.ReplaceAll(module, ".", "/"))
		paths = append(paths, name+".py", name+".pyi", path.Join(name, "__init__.py"), path.Join(name, "__init__.pyi"))
		// from package import child may name a submodule or an exported value.
		// Keep both as candidates rather than assume package execution semantics.
		if imp.From != "" && imp.Path != imp.From {
			child := strings.TrimPrefix(imp.Path, imp.From+".")
			if child != "*" && child != imp.Path {
				paths = append(paths, path.Join(name, child)+".py", path.Join(name, child, "__init__.py"))
			}
		}
	} else if extract.ModuleLanguage(f.Language) {
		if !strings.HasPrefix(imp.Path, "./") && !strings.HasPrefix(imp.Path, "../") {
			return nil
		}
		name := path.Join(path.Dir(f.Path), imp.Path)
		paths = append(paths, name)
		if path.Ext(name) == "" {
			for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"} {
				paths = append(paths, name+ext, path.Join(name, "index"+ext))
			}
		}
	}
	valid := paths[:0]
	for _, candidate := range paths {
		if fs.ValidPath(candidate) {
			valid = append(valid, candidate)
		}
	}
	return valid
}

func resolveModule(ctx context.Context, f extract.Facts, files map[string]extract.Facts, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	if !extract.ModuleLanguage(f.Language) {
		return edges, issues, nil
	}
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, imp := range f.Imports {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var targets []string
		for _, candidate := range ImportPaths(f, imp) {
			if target, ok := files[candidate]; ok && compatibleLanguage(f.Language, target.Language) {
				targets = append(targets, candidate)
			}
		}
		confidence := "exact"
		if len(targets) == 0 {
			issues = append(issues, Issue{f.Path, "unresolved_import", imp.Path, "imports", imp.Span})
		} else if len(targets) > 1 || (f.Language == "python" && imp.Relative == 0) {
			confidence = "candidate"
		}
		for _, target := range targets {
			if err := add(Edge{Ref{f.Path, -1}, Ref{target, -1}, "imports", confidence, "source_module", f.Path, imp.Span}); err != nil {
				return nil, nil, err
			}
		}
	}
	binder := moduleBinder{ctx, files, limit - len(edges)}
	symbolEdges, gaps, err := binder.importEdges(f)
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, symbolEdges...)
	issues = append(issues, gaps...)
	for _, call := range f.Calls {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		source := Ref{f.Path, -1}
		best := len(f.Source) + 1
		var targets []Ref
		for i, d := range f.Declarations {
			if d.Start <= call.Start && d.End >= call.End && d.End-d.Start < best {
				source.Declaration = i
				best = d.End - d.Start
			}
			if d.Parent == -1 && d.Kind == "function" && d.Name == call.Name {
				targets = append(targets, Ref{f.Path, i})
			}
		}
		supplement, err := resolveCallTargets(ctx, f, call, files, "", limit-len(edges))
		if err != nil {
			return nil, nil, err
		}
		if len(supplement) > 0 {
			edges = append(edges, supplement...)
			continue
		}
		if call.Imported {
			imported, _, err := binder.useTargets(f, call.Name, call.Receiver, call.Span)
			if err != nil {
				return nil, nil, err
			}
			found := false
			for _, target := range imported {
				if files[target.Path].Declarations[target.Declaration].Kind != "function" {
					continue
				}
				found = true
				if err := add(Edge{source, target.Ref, "calls", target.Confidence, "imported_binding", f.Path, call.Span}); err != nil {
					return nil, nil, err
				}
			}
			if !found {
				issues = append(issues, Issue{f.Path, "unresolved_call", call.Name, "calls", call.Span})
			}
			continue
		}
		if call.Blocked || call.Receiver != "" {
			issues = append(issues, Issue{f.Path, "dynamic_call", call.Name, "calls", call.Span})
			continue
		}
		if len(targets) == 0 {
			issues = append(issues, Issue{f.Path, "unresolved_call", call.Name, "calls", call.Span})
			continue
		}
		confidence := "exact"
		if len(targets) > 1 {
			confidence = "candidate"
		}
		for _, target := range targets {
			if err := add(Edge{source, target, "calls", confidence, "module_function", f.Path, call.Span}); err != nil {
				return nil, nil, err
			}
		}
	}
	return edges, issues, nil
}

func compatibleLanguage(a, b string) bool {
	return a == b || a != "python" && b != "python" && extract.ModuleLanguage(a) && extract.ModuleLanguage(b)
}
