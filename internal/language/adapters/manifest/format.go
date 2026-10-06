package manifest

import "path"

// Format recognizes a format, not a Component or an import namespace.
func Format(name string) string {
	switch path.Base(name) {
	case "go.mod":
		return "gomod"
	case "pyproject.toml":
		return "pyproject"
	case "package.json":
		return "package_json"
	default:
		return ""
	}
}
