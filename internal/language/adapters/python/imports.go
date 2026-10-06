package python

import (
	"io/fs"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func (Adapter) ImportPaths(f analysis.Facts, imp analysis.Import) []string {
	var paths []string
	module := imp.Path
	if imp.From != "" {
		module = imp.From
	}
	bases := []string{"."}
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
	if imp.Relative > 0 {
		bases = []string{base}
	} else {
		// The importing file can anchor its own top-level package below a
		// source/SDK directory. This proposes layouts, not sys.path: keep
		// the snapshot-root candidate and never search unrelated files.
		first, _, _ := strings.Cut(module, ".")
		for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
			if path.Base(dir) == first && path.Dir(dir) != "." {
				bases = append(bases, path.Dir(dir))
			}
		}
	}
	for _, base := range bases {
		name := path.Join(base, strings.ReplaceAll(module, ".", "/"))
		paths = append(paths, name+".py", name+".pyi", path.Join(name, "__init__.py"), path.Join(name, "__init__.pyi"))
		// from package import child may name a submodule or an exported value.
		// Keep both as candidates rather than assume package execution semantics.
		if imp.From != "" && imp.Path != imp.From {
			child := strings.TrimPrefix(imp.Path, imp.From+".")
			bareRelative := imp.Relative > 0 && strings.Trim(imp.From, ".") == ""
			if bareRelative {
				child = imp.Path
			}
			if child != "*" && (bareRelative || child != imp.Path) {
				paths = append(paths, path.Join(name, child)+".py", path.Join(name, child, "__init__.py"))
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
