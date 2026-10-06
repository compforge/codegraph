// Package manifest interprets selected project manifests on gotreesitter trees.
// It never opens files or evaluates package-manager configuration.
package manifest

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type Adapter struct{}

func (Adapter) Extract(ctx context.Context, f analysis.Facts, tree *gts.Tree, entry grammars.LangEntry) (analysis.Facts, error) {
	x := extraction{f: f, lang: tree.Language(), seen: map[string]bool{}}
	switch Format(f.Path) {
	case "gomod":
		x.f.Manifest = &analysis.Manifest{Format: "gomod"}
		x.gomod(tree.RootNode())
	case "package_json":
		x.f.Manifest = &analysis.Manifest{Format: "package_json"}
		x.json(tree.RootNode())
	case "pyproject":
		x.f.Manifest = &analysis.Manifest{Format: "pyproject"}
		x.toml(tree.RootNode(), nil)
	}
	return x.f, ctx.Err()
}

type extraction struct {
	f    analysis.Facts
	lang *gts.Language
	seen map[string]bool
}

func span(n *gts.Node) analysis.Span {
	return analysis.Span{Start: int(n.StartByte()), End: int(n.EndByte())}
}
func (x *extraction) issue(n *gts.Node, message string) {
	x.f.Issues = append(x.f.Issues, analysis.Issue{Code: "unsupported_manifest", Subject: "document", Message: message, Span: span(n)})
}
func (x *extraction) value(key string, n *gts.Node, value string, ok bool) {
	m := x.f.Manifest
	if x.seen[key] {
		// Duplicate declarations cannot select an arbitrary module or project identity.
		x.issue(n, "duplicate manifest "+key)
		value, ok = "", false
	}
	x.seen[key] = true
	if !ok {
		x.issue(n, "manifest "+key+" requires a supported static string")
	}
	switch key {
	case "name":
		m.Name, m.NameSpan = value, span(n)
	case "version":
		m.Version, m.VersionSpan = value, span(n)
	}
}
func (x *extraction) gomod(root *gts.Node) {
	for i := 0; i < root.NamedChildCount(); i++ {
		directive := root.NamedChild(i)
		if directive.Type(x.lang) != "module_directive" {
			continue
		}
		for j := 0; j < directive.NamedChildCount(); j++ {
			n := directive.NamedChild(j)
			if n.Type(x.lang) != "module_path" {
				continue
			}
			value := n.Text(x.f.Source)
			ok := true
			if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "`") {
				var err error
				value, err = strconv.Unquote(value)
				ok = err == nil
			}
			x.f.Manifest.Project = true
			x.value("name", n, value, ok)
		}
	}
	if !x.seen["name"] {
		x.issue(root, "Go module manifest has no module declaration")
	}
}
func (x *extraction) json(root *gts.Node) {
	var object *gts.Node
	for i := 0; i < root.NamedChildCount(); i++ {
		if n := root.NamedChild(i); n.Type(x.lang) == "object" {
			object = n
			break
		}
	}
	if object == nil {
		x.issue(root, "package.json must contain an object")
		return
	}
	for i := 0; i < object.NamedChildCount(); i++ {
		pair := object.NamedChild(i)
		if pair.Type(x.lang) != "pair" || pair.NamedChildCount() < 2 {
			continue
		}
		key, value := pair.NamedChild(0), pair.NamedChild(1)
		var name string
		if json.Unmarshal([]byte(key.Text(x.f.Source)), &name) != nil {
			continue
		}
		switch name {
		case "name", "version":
			if name == "name" {
				x.f.Manifest.Project = true
			}
			var v string
			ok := value.Type(x.lang) == "string" && json.Unmarshal([]byte(value.Text(x.f.Source)), &v) == nil
			x.value(name, value, v, ok)
		case "workspaces":
			x.f.Manifest.Workspace = true
		}
	}
}

// Keys are decoded by syntax nodes, so quoted keys and dotted/inline tables
// select the same metadata as ordinary [project] sections.
func (x *extraction) keys(n *gts.Node) ([]string, bool) {
	switch n.Type(x.lang) {
	case "bare_key":
		return []string{n.Text(x.f.Source)}, true
	case "quoted_key":
		s, ok := tomlString(n.Text(x.f.Source))
		return []string{s}, ok
	case "dotted_key":
		var out []string
		for i := 0; i < n.NamedChildCount(); i++ {
			k, ok := x.keys(n.NamedChild(i))
			if !ok {
				return nil, false
			}
			out = append(out, k...)
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}
func (x *extraction) section(keys []string) {
	if len(keys) == 0 {
		return
	}
	if keys[0] == "project" {
		x.f.Manifest.Project = true
	}
	if keys[0] == "build-system" {
		x.f.Manifest.BuildSystem = true
	}
	if len(keys) >= 3 && keys[0] == "tool" && keys[1] == "uv" && keys[2] == "workspace" {
		x.f.Manifest.Workspace = true
	}
}
func (x *extraction) toml(n *gts.Node, prefix []string) {
	switch n.Type(x.lang) {
	case "table":
		if n.NamedChildCount() == 0 {
			return
		}
		keys, ok := x.keys(n.NamedChild(0))
		if !ok {
			x.issue(n, "unsupported manifest table key")
			return
		}
		x.section(keys)
		for i := 1; i < n.NamedChildCount(); i++ {
			x.toml(n.NamedChild(i), keys)
		}
		return
	case "table_array_element":
		return // Arrays of tables do not declare any of the selected project fields.
	case "pair":
		if n.NamedChildCount() < 2 {
			return
		}
		keys, ok := x.keys(n.NamedChild(0))
		if !ok {
			x.issue(n, "unsupported manifest key")
			return
		}
		full := append(append([]string{}, prefix...), keys...)
		v := n.NamedChild(1)
		if len(full) > 1 {
			x.section(full[:len(full)-1])
		}
		if v.Type(x.lang) == "inline_table" {
			x.section(full)
			for i := 0; i < v.NamedChildCount(); i++ {
				x.toml(v.NamedChild(i), full)
			}
		}
		if len(full) == 2 && full[0] == "project" && (full[1] == "name" || full[1] == "version") {
			x.f.Manifest.Project = true
			value, ok := tomlString(v.Text(x.f.Source))
			x.value(full[1], v, value, v.Type(x.lang) == "string" && ok)
		}
		return
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		x.toml(n.NamedChild(i), prefix)
	}
}

// Decode single-line TOML strings only after grammar validation. Multiline
// metadata remains an explicit coverage gap instead of being guessed or erased.
func tomlString(raw string) (string, bool) {
	if strings.HasPrefix(raw, "\"\"\"") || strings.HasPrefix(raw, "'''") {
		return "", false
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], true
	}
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	s, err := strconv.Unquote(raw)
	return s, err == nil
}

func Describe(entry grammars.LangEntry) analysis.Capability {
	c := analysis.Capability{Language: entry.Name, Limitations: []string{"manifest metadata extraction applies only to go.mod, pyproject.toml and package.json", "explicit name/version and project/build/workspace section presence only; dependency constraints and package-manager execution are not modeled", "multiline TOML metadata strings are diagnosed, not decoded"}}
	if entry.Name == "gomod" {
		c.Organizations = []string{"Module"}
		c.Relations = []string{"declares", "in_namespace"}
	}
	return c
}
