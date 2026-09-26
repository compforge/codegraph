// Package resolve binds extracted references within an explicitly supplied scope.
package resolve

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/compforge/codegraph/internal/extract"
)

var ErrEdgeLimit = errors.New("relation limit reached")

type Ref struct {
	Path        string
	Declaration int
} // -1 identifies the file
type Edge struct {
	Source, Target          Ref
	Kind, Confidence, Basis string
	// Path owns the evidence span; a cross-file contains edge is located at its method.
	Path string
	Span extract.Span
}
type Issue struct {
	Path, Code, Reference, Relation string
	Span                            extract.Span
}

func ImportDir(module, imp string) (string, bool) {
	if module == "" {
		return "", false
	}
	if imp == module {
		return ".", true
	}
	if strings.HasPrefix(imp, module+"/") {
		return strings.TrimPrefix(imp, module+"/"), true
	}
	return "", false
}

// Resolve retains ambiguity as candidate edges; it never guesses receiver types.
func Resolve(ctx context.Context, files map[string]extract.Facts, module string, limit int) ([]Edge, []Issue, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	owners := receiverIndex(files, names)
	var edges []Edge
	var issues []Issue
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := files[name]
		if f.Language != "go" {
			found, gaps, err := resolveModule(ctx, f, files, limit-len(edges))
			if err != nil {
				return nil, nil, err
			}
			edges = append(edges, found...)
			issues = append(issues, gaps...)
			continue
		}
		for i, d := range f.Declarations {
			if d.Receiver == "" {
				continue
			}
			var targets []Ref
			for _, target := range owners[receiverKey{path.Dir(name), f.Package, d.Receiver}] {
				if strings.HasSuffix(target.Path, "_test.go") && !strings.HasSuffix(name, "_test.go") {
					continue
				}
				targets = append(targets, target)
			}
			confidence := "exact"
			if len(targets) == 0 {
				issues = append(issues, Issue{name, "unresolved_receiver", d.Receiver, "contains", d.Span})
			} else if len(targets) > 1 {
				confidence = "candidate"
			}
			for _, owner := range targets {
				if err := add(Edge{owner, Ref{name, i}, "contains", confidence, "receiver_declaration", name, d.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
		imports := map[string][]string{}
		for _, imp := range f.Imports {
			dir, local := ImportDir(module, imp.Path)
			var targets []string
			if local {
				for _, n := range names {
					if files[n].Language == "go" && path.Dir(n) == dir && !strings.HasSuffix(n, "_test.go") {
						targets = append(targets, n)
					}
				}
			}
			if len(targets) == 0 {
				issues = append(issues, Issue{name, "unresolved_import", imp.Path, "imports", imp.Span})
			}
			for _, target := range targets {
				if err := add(Edge{Ref{name, -1}, Ref{target, -1}, "imports", "exact", "module_import", name, imp.Span}); err != nil {
					return nil, nil, err
				}
				alias := imp.Alias
				if alias == "" {
					alias = files[target].Package
				}
				imports[alias] = append(imports[alias], target)
			}
		}
		for _, call := range f.Calls {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			source := Ref{name, -1}
			best := len(f.Source) + 1
			for i, d := range f.Declarations {
				if d.Start <= call.Start && d.End >= call.End && d.End-d.Start < best {
					source.Declaration = i
					best = d.End - d.Start
				}
			}
			refname := call.Name
			if call.Receiver != "" {
				refname = call.Receiver + "." + call.Name
			}
			supplement, err := resolveCallTargets(ctx, f, call, files, module, limit-len(edges))
			if err != nil {
				return nil, nil, err
			}
			if len(supplement) > 0 {
				edges = append(edges, supplement...)
				continue
			}
			if call.Blocked {
				issues = append(issues, Issue{name, "dynamic_call", refname, "calls", call.Span})
				continue
			}
			var candidates []Ref
			candidateFiles := imports[call.Receiver]
			basis := "imported_function"
			if call.Receiver == "" {
				basis = "package_function"
				candidateFiles = nil
				for _, n := range names {
					if files[n].Language == "go" && path.Dir(n) == path.Dir(name) && files[n].Package == f.Package {
						if strings.HasSuffix(n, "_test.go") && !strings.HasSuffix(name, "_test.go") {
							continue
						}
						candidateFiles = append(candidateFiles, n)
					}
				}
				candidateFiles = append(candidateFiles, imports["."]...)
			}
			seen := map[Ref]bool{}
			for _, n := range candidateFiles {
				for i, d := range files[n].Declarations {
					if d.Name != call.Name || d.Kind != "function" {
						continue
					}
					if n != name && path.Dir(n) != path.Dir(name) && !exported(d.Name) {
						continue
					}
					r := Ref{n, i}
					if !seen[r] {
						candidates = append(candidates, r)
						seen[r] = true
					}
				}
			}
			if len(candidates) == 0 {
				if !call.Builtin {
					issues = append(issues, Issue{name, "unresolved_call", refname, "calls", call.Span})
				}
				continue
			}
			confidence := "exact"
			if len(candidates) > 1 {
				confidence = "candidate"
			}
			for _, target := range candidates {
				if err := add(Edge{source, target, "calls", confidence, basis, name, call.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	types, typeGaps, err := resolveTypeRelations(ctx, files, names, module, limit-len(edges))
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, types...)
	issues = append(issues, typeGaps...)
	references, gaps, err := resolveReferences(ctx, files, names, module, limit-len(edges))
	if err != nil {
		return nil, nil, err
	}
	edges = append(edges, references...)
	issues = append(issues, gaps...)
	return edges, issues, nil
}

func exported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}
