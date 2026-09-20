package resolve

import (
	"path"

	"github.com/compforge/codegraph/internal/extract"
)

type receiverKey struct{ directory, pkg, name string }

// A receiver names a package-level type even when the method and type live in
// different files. Lexically nested types with that name are not candidates.
// This binds declaration ownership only; it does not resolve receiver dispatch.
func receiverIndex(files map[string]extract.Facts, names []string) map[receiverKey][]Ref {
	index := map[receiverKey][]Ref{}
	for _, name := range names {
		f := files[name]
		for i, d := range f.Declarations {
			if d.Parent != -1 {
				continue
			}
			switch d.Kind {
			case "struct", "interface", "type", "type_alias":
				key := receiverKey{path.Dir(name), f.Package, d.Name}
				index[key] = append(index[key], Ref{name, i})
			}
		}
	}
	return index
}
