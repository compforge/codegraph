package language

import "github.com/compforge/codegraph/internal/language/adapters/manifest"

// MaterialKind classifies the role of supplied material independently of grammar.
// A parser for JSON/TOML does not make every data file a project manifest.
func MaterialKind(name, gitlink string) string {
	if gitlink != "" {
		return "gitlink"
	}
	if manifest.Format(name) != "" {
		return "manifest"
	}
	entry := Detect(name)
	if entry == nil {
		return "unknown"
	}
	switch entry.Name {
	case "json", "json5", "jsonnet", "toml", "yaml", "xml", "csv", "ini":
		return "unknown"
	default:
		return "source"
	}
}
