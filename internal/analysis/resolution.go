package analysis

import (
	"path"
	"strings"
)

type ImportKey struct {
	Document            string
	Start               int
	Path, From, Binding string
}
type ImportResolution struct {
	Targets    []string
	Confidence Confidence
	Basis      string
}
type ResolutionContext struct {
	GoModules map[string]string
	Imports   map[ImportKey]ImportResolution
}

func (r ResolutionContext) Import(f Facts, i Import) (ImportResolution, bool) {
	v, ok := r.Imports[ImportKey{Document: f.Path, Start: i.Span.Start, Path: i.Path, From: i.From, Binding: i.Binding}]
	return v, ok
}

// GoImportDir chooses the most specific module path. Nested module roots also
// bound ownership: a parent module cannot claim source inside a child module.
func (r ResolutionContext) GoImportDir(fallback, imported string) (string, bool) {
	root, best := "", ""
	for candidate, module := range r.GoModules {
		if imported == module || strings.HasPrefix(imported, module+"/") {
			if len(module) > len(best) || module == best && candidate < root {
				root, best = candidate, module
			}
		}
	}
	if best == "" {
		if len(r.GoModules) > 0 || fallback == "" {
			return "", false
		}
		root, best = ".", fallback
		if imported != best && !strings.HasPrefix(imported, best+"/") {
			return "", false
		}
	}
	dir := path.Join(root, strings.TrimPrefix(strings.TrimPrefix(imported, best), "/"))
	for other := range r.GoModules {
		if other != root && (root == "." || strings.HasPrefix(other, root+"/")) && (dir == other || strings.HasPrefix(dir, other+"/")) {
			return "", false
		}
	}
	return dir, true
}

// GoModule returns the supplied module owning dir. A nested module is an
// independent owner, not a child namespace of its enclosing filesystem module.
func (r ResolutionContext) GoModule(fallback, dir string) (root, module string, ok bool) {
	for candidate, value := range r.GoModules {
		if candidate == "." || dir == candidate || strings.HasPrefix(dir, candidate+"/") {
			if root == "" || candidate != "." && (root == "." || len(candidate) > len(root)) {
				root, module = candidate, value
			}
		}
	}
	if root != "" {
		return root, module, true
	}
	if fallback != "" {
		return ".", fallback, true
	}
	return "", "", false
}

func (r ResolutionContext) GoPackage(module, dir, pkg string) string {
	if root, name, ok := r.GoModule(module, dir); ok {
		suffix := dir
		if root != "." {
			suffix = strings.TrimPrefix(strings.TrimPrefix(dir, root), "/")
		}
		return path.Join(name, suffix)
	}
	return dir + ":" + pkg
}
