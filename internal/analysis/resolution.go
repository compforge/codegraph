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
func (r ResolutionContext) GoPackage(module, dir, pkg string) string {
	root, best := "", ""
	for candidate, value := range r.GoModules {
		if candidate == "." || dir == candidate || strings.HasPrefix(dir, candidate+"/") {
			if root == "" || len(candidate) > len(root) {
				root, best = candidate, value
			}
		}
	}
	if root != "" {
		suffix := dir
		if root != "." {
			suffix = strings.TrimPrefix(strings.TrimPrefix(dir, root), "/")
		}
		return path.Join(best, suffix)
	}
	if module != "" {
		return path.Join(module, dir)
	}
	return dir + ":" + pkg
}
