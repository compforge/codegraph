package ecmascript

import (
	"io/fs"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

func (Adapter) ImportPaths(f analysis.Facts, imp analysis.Import) []string {
	var paths []string
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
	valid := paths[:0]
	for _, candidate := range paths {
		if fs.ValidPath(candidate) {
			valid = append(valid, candidate)
		}
	}
	return valid
}
