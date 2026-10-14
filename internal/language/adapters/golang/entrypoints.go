package golang

import gts "github.com/odvcencio/gotreesitter"

// +why=Runtime entry classification consumes grammar-owned signatures; consumers own entry filtering.
func markGoEntrypoints(f *Facts, signatures []gts.SignatureFact) {
	entries := map[int]bool{}
	for _, s := range signatures {
		if s.Kind == "function" && len(s.TypeParameters) == 0 && len(s.Parameters) == 0 && len(s.Results) == 0 &&
			(s.Name == "init" || s.Name == "main" && f.Package == "main") {
			entries[int(s.StartByte)] = true
		}
	}
	for i := range f.Declarations {
		f.Declarations[i].Entrypoint = entries[f.Declarations[i].Start]
	}
}
