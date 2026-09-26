package resolve

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

func goTypeTargets(f extract.Facts, name, qualifier string, files map[string]extract.Facts, names []string, module string) []bindingTarget {
	dir := path.Dir(f.Path)
	pkg := f.Package
	if qualifier != "" {
		dir = ""
		for _, imp := range f.Imports {
			imported, ok := ImportDir(module, imp.Path)
			if !ok {
				continue
			}
			for _, p := range names {
				target := files[p]
				alias := imp.Alias
				if alias == "" {
					alias = target.Package
				}
				if target.Language == "go" && path.Dir(p) == imported && alias == qualifier {
					dir = imported
					pkg = target.Package
				}
			}
		}
	}
	var out []bindingTarget
	for _, p := range names {
		target := files[p]
		if dir == "" || target.Language != "go" || path.Dir(p) != dir || target.Package != pkg || strings.HasSuffix(p, "_test.go") && !strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		for i, d := range target.Declarations {
			if d.Parent == -1 && d.Name == name && (qualifier == "" || exported(name)) {
				switch d.Kind {
				case "struct", "interface", "type", "type_alias":
					out = append(out, bindingTarget{Ref{p, i}, "exact"})
				}
			}
		}
	}
	return uniqueBindingTargets(out)
}

func resolveTypeRelations(ctx context.Context, files map[string]extract.Facts, names []string, module string, limit int) ([]Edge, []Issue, error) {
	var edges []Edge
	var issues []Issue
	add := func(e Edge) error {
		if len(edges) >= limit {
			return ErrEdgeLimit
		}
		edges = append(edges, e)
		return nil
	}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := files[p]
		for _, hint := range f.TypeRelations {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			var targets []bindingTarget
			if f.Language == "go" {
				targets = goTypeTargets(f, hint.Name, hint.Module, files, names, module)
			} else {
				binder := moduleBinder{ctx, files, limit - len(edges)}
				imported, matched, err := binder.useTargets(f, hint.Name, hint.Module, hint.Span)
				if err != nil {
					return nil, nil, err
				}
				targets = imported
				if !matched && hint.Module == "" {
					// A nested base declaration must be visible from the derived type's scope.
					scope := f.Declarations[hint.Owner].Parent
					for scope >= -1 {
						for i, d := range f.Declarations {
							if d.Parent == scope && d.Name == hint.Name && (d.Kind == "class" || d.Kind == "interface") {
								targets = append(targets, bindingTarget{Ref{p, i}, "exact"})
							}
						}
						if len(targets) > 0 || scope == -1 {
							break
						}
						scope = f.Declarations[scope].Parent
					}
				}
			}
			var eligible []bindingTarget
			for _, target := range targets {
				if target.Declaration < 0 {
					continue
				}
				kind := files[target.Path].Declarations[target.Declaration].Kind
				sourceKind := f.Declarations[hint.Owner].Kind
				allowed := kind == "class" || kind == "interface"
				if f.Language == "go" || hint.Kind == "implements" || sourceKind == "interface" {
					allowed = kind == "interface"
				}
				if allowed {
					eligible = append(eligible, target)
				}
			}
			eligible = uniqueBindingTargets(eligible)
			if len(eligible) == 0 {
				issues = append(issues, Issue{p, "unresolved_type_relation", hint.Name, hint.Kind, hint.Span})
			}
			for _, target := range eligible {
				if err := add(Edge{Ref{p, hint.Owner}, target.Ref, hint.Kind, target.Confidence, hint.Basis, p, hint.Span}); err != nil {
					return nil, nil, err
				}
			}
		}
		if f.Language != "go" {
			continue
		}
		for i, d := range f.Declarations {
			if d.Parent != -1 || d.Kind != "struct" && d.Kind != "type" {
				continue
			}
			methods := map[string]bool{}
			for _, q := range names {
				other := files[q]
				if other.Language != "go" || path.Dir(q) != path.Dir(p) || other.Package != f.Package || strings.HasSuffix(q, "_test.go") && !strings.HasSuffix(p, "_test.go") {
					continue
				}
				for _, m := range other.Declarations {
					if m.Kind == "method" && m.Receiver == d.Name {
						methods[m.Name] = true
					}
				}
			}
			for _, q := range names {
				other := files[q]
				if other.Language != "go" || path.Dir(q) != path.Dir(p) || other.Package != f.Package || strings.HasSuffix(q, "_test.go") && !strings.HasSuffix(p, "_test.go") {
					continue
				}
				for j, iface := range other.Declarations {
					if err := ctx.Err(); err != nil {
						return nil, nil, err
					}
					if iface.Kind != "interface" || iface.Parent != -1 {
						continue
					}
					// Empty interfaces and embedded/type-term interfaces would yield overly
					// broad matches from this direct-method vocabulary; exclude them from inference.
					complete := true
					for _, h := range other.TypeRelations {
						if h.Owner == j {
							complete = false
						}
					}
					for _, issue := range other.Issues {
						if issue.Relation == "extends" && issue.Start >= iface.Start && issue.End <= iface.End {
							complete = false
						}
					}
					count := 0
					for _, m := range other.Declarations {
						if m.Kind == "method" && m.Parent == j {
							count++
							if !methods[m.Name] {
								complete = false
							}
						}
					}
					if !complete || count == 0 {
						continue
					}
					if err := add(Edge{Ref{p, i}, Ref{q, j}, "implements", "candidate", "method_name_set", p, d.Span}); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}
	return edges, issues, nil
}
