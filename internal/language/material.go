package language

// MaterialKind classifies the role of supplied material independently of grammar.
// A parser for JSON/TOML does not make every data file a project manifest.
func MaterialKind(name, gitlink string) string {
	if gitlink != "" {
		return "gitlink"
	}
	entry := Detect(name)
	if entry == nil {
		return "unknown"
	}
	switch entry.Name {
	case "gomod", "json", "json5", "jsonnet", "toml", "yaml", "xml", "csv", "ini":
		return "unknown"
	default:
		return "source"
	}
}
