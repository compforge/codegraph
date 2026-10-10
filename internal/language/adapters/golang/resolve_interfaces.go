package golang

import (
	"context"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

// resolveInterfaceCalls joins observed construction arguments with resolved
// interface calls. The join is by interface identity, not object identity: it
// supplies possible targets, never proof that a particular instance flows to a
// particular call. Keep the interface target and mark the additional evidence
// heuristic; same-named methods without an observed argument are not candidates.
func resolveInterfaceCalls(ctx context.Context, files map[string]analysis.Facts, calls []Edge, module string, methods *methodIndex, limit int) ([]Edge, error) {
	type site struct {
		path string
		span Span
	}
	arguments := map[site][]*GoType{}
	for name, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, call := range f.Calls {
			hints := usage(call.Extension).Arguments
			for _, hint := range hints {
				if hint != nil {
					arguments[site{name, call.Span}] = hints
					break
				}
			}
		}
	}
	type candidate struct {
		method    Ref
		testOnly  bool
		injection analysis.SourceLocation
	}
	implementations := map[Ref][]candidate{}
	count := 0
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		args := arguments[site{call.Path, call.Span}]
		if len(args) == 0 {
			continue
		}
		callee := files[call.Target.Path]
		declaration := callee.Declarations[call.Target.Declaration]
		// Only statically resolved functions bind positional arguments in this first
		// increment. Methods, callable aliases and variadics keep their existing gaps.
		if declaration.Kind != "function" || call.Basis != "imported_function" && call.Basis != "package_function" {
			continue
		}
		signature := typeShape(declaration.Extension)
		if signature == nil || len(signature.Parameters) != len(args) {
			continue
		}
		caller := files[call.Path]
		for slot, arg := range args {
			if arg == nil {
				continue
			}
			parameters := goNamedTypeRefs(callee, signature.Parameters[slot], files, module, methods)
			concrete := goNamedTypeRefs(caller, arg, files, module, methods)
			for _, parameter := range parameters {
				owner := files[parameter.Path]
				if owner.Declarations[parameter.Declaration].Kind != "interface" {
					continue
				}
				for i, member := range owner.Declarations {
					if member.Parent != parameter.Declaration || member.Kind != "method" {
						continue
					}
					// Unexported methods have package identity, so an argument from another
					// package cannot satisfy them by spelling alone.
					if !exported(member.Name) {
						continue
					}
					contract := analysis.SourceRef(parameter.Path, i)
					for _, root := range concrete {
						kind := files[root.Path].Declarations[root.Declaration].Kind
						if kind != "struct" && kind != "type" {
							continue
						}
						targets, err := methods.lookup(ctx, []Ref{root}, member.Name, strings.HasSuffix(caller.Path, "_test.go"), limit)
						if err != nil {
							return nil, err
						}
						for _, target := range targets {
							entry := candidate{target.Ref, strings.HasSuffix(caller.Path, "_test.go") || strings.HasSuffix(target.Path, "_test.go"), analysis.SourceLocation{Path: call.Path, Span: call.Span}}
							duplicate := false
							for _, old := range implementations[contract] {
								if old.method == entry.method && old.testOnly == entry.testOnly {
									duplicate = true
									break
								}
							}
							if duplicate {
								continue
							}
							if count >= limit {
								return nil, ErrEdgeLimit
							}
							implementations[contract] = append(implementations[contract], entry)
							count++
						}
					}
				}
			}
		}
	}
	var out []Edge
	type proof struct {
		site           site
		source, target Ref
	}
	seen := map[proof]bool{}
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, candidate := range implementations[call.Target] {
			if candidate.testOnly && !strings.HasSuffix(call.Path, "_test.go") {
				continue
			}
			key := proof{site{call.Path, call.Span}, call.Source, candidate.method}
			if seen[key] {
				continue
			}
			if len(out) >= limit {
				return nil, ErrEdgeLimit
			}
			seen[key] = true
			out = append(out, Edge{Source: call.Source, Target: candidate.method, Kind: "calls", Confidence: analysis.Heuristic, Basis: "interface_argument", Path: call.Path, Span: call.Span, Evidence: []analysis.Evidence{{Basis: "interface_argument", Confidence: analysis.Heuristic, Location: &candidate.injection}}})
		}
	}
	return out, nil
}

func goNamedTypeRefs(source analysis.Facts, hint *GoType, files map[string]analysis.Facts, module string, methods *methodIndex) []Ref {
	if hint == nil {
		return nil
	}
	if hint.Bound {
		if hint.Target >= 0 {
			return []Ref{analysis.SourceRef(source.Path, hint.Target)}
		}
		return nil
	}
	var out []Ref
	for _, target := range goTypeTargets(source, hint.Name, hint.Module, files, methods.namespaces, module) {
		out = append(out, target.Ref)
	}
	return out
}
