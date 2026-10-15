package language

import (
	"path"
	"strings"
)

// MaterialTag classifies recognized file formats independently of grammar support.
// An unrecognized format carries no material tag; it is not assumed to be source.
func MaterialTag(name string) string {
	entry := Detect(name)
	if entry == nil {
		switch strings.ToLower(path.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".pdf":
			return "resource"
		}
		return ""
	}
	switch entry.Name {
	case "markdown", "markdown_inline", "rst", "vimdoc":
		return "documentation"
	case "gomod", "json", "json5", "jsonnet", "toml", "yaml", "xml", "csv", "ini", "editorconfig", "html", "css", "scss":
		return "resource"
	default:
		return "source"
	}
}
