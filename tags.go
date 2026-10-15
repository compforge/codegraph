package codegraph

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/language"
)

// Tag is an open classification vocabulary. Applications may define additional
// values without registration; tags do not select parsers or exclusion policy.
type Tag string

const (
	SourceTag        Tag = "source"
	DocumentationTag Tag = "documentation"
	ResourceTag      Tag = "resource"
	ToolingTag       Tag = "tooling"
	ConfigTag        Tag = "config"
	ManifestTag      Tag = "manifest"
	LockfileTag      Tag = "lockfile"
	GeneratedTag     Tag = "generated"
	TestFixtureTag   Tag = "test_fixture"
	DependencyTag    Tag = "dependency"
	BuildOutputTag   Tag = "build_output"
	CacheTag         Tag = "cache"
	MinifiedTag      Tag = "minified"
)

// TagRule assigns Name to each Document or Directory whose snapshot-relative
// slash-separated path matches Pattern. Directory paths have no trailing slash;
// the root is ".". Patterns use Go regular expressions (unanchored unless specified).
// +spec=All matching rules accumulate by name, with no precedence or inheritance.
type TagRule struct {
	Name    Tag
	Pattern string
}

// BuiltinTagRules returns a fresh rule set for common repository material.
// Callers may append custom rules or supply a replacement through Options.TagRules.
// Tags describe path conventions; consumers decide whether to exclude material.
func BuiltinTagRules() []TagRule {
	return []TagRule{
		{Name: ManifestTag, Pattern: `(^|/)(go\.mod|go\.work|pyproject\.toml|package\.json)$`},
		{Name: LockfileTag, Pattern: `(^|/)(go\.sum|go\.work\.sum|uv\.lock|poetry\.lock|Pipfile\.lock|pdm\.lock|pylock(\.[^/]+)?\.toml|package-lock\.json|npm-shrinkwrap\.json|pnpm-lock\.yaml|yarn\.lock|bun\.lockb?|Cargo\.lock|Gemfile\.lock|composer\.lock)$`},
		{Name: GeneratedTag, Pattern: `\.(generated\.[^/]+|gen\.go|pb\.go)$`},
		{Name: GeneratedTag, Pattern: `(^|/)kitex_gen(/|$)`},
		{Name: TestFixtureTag, Pattern: `(^|/)(testdata|fixtures|snapshots|__snapshots__)(/|$)`},
		{Name: DependencyTag, Pattern: `(^|/)(vendor|node_modules)(/|$)`},
		{Name: BuildOutputTag, Pattern: `(^|/)(dist|\.next)(/|$)`},
		{Name: CacheTag, Pattern: `(^|/)(\.cache|__pycache__|\.pytest_cache|\.mypy_cache|\.ruff_cache)(/|$)`},
		{Name: MinifiedTag, Pattern: `\.min\.(js|css)$`},
		{Name: ToolingTag, Pattern: `(^|/)(\.git|\.idea|\.vscode|\.codebase|\.devloop|\.codex|\.claude|\.agents|\.cursor)(/|$)`},
		{Name: ConfigTag, Pattern: `(^|/)(\.dockerignore|\.gitignore|\.ignore|\.gitattributes|\.editorconfig|\.env(\.[^/]+)?)$`},
		{Name: ToolingTag, Pattern: `(^|/)(AGENTS|CLAUDE|GEMINI)\.md$`},
	}
}

type compiledTagRule struct {
	name    Tag
	pattern *regexp.Regexp
}

// TagMatcher applies compiled path rules without reading files or constructing a graph.
// It is immutable after construction and safe for concurrent Match calls.
// The zero value has no path rules; DocumentTags still classifies formats.
type TagMatcher struct {
	rules []compiledTagRule
}

// NewTagMatcher validates and compiles rules. Nil selects BuiltinTagRules;
// an explicit empty slice disables path tags. Later changes to rules do not affect
// the matcher. Invalid regular expressions and blank names return an error.
// +why=Graph construction and consumers share one path classification implementation.
func NewTagMatcher(rules []TagRule) (*TagMatcher, error) {
	if rules == nil {
		rules = BuiltinTagRules()
	}
	out := make([]compiledTagRule, 0, len(rules))
	for i, rule := range rules {
		if strings.TrimSpace(string(rule.Name)) == "" {
			return nil, fmt.Errorf("tag rule %d: name is required", i)
		}
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("tag rule %d (%q): %w", i, rule.Name, err)
		}
		out = append(out, compiledTagRule{name: rule.Name, pattern: pattern})
	}
	return &TagMatcher{rules: out}, nil
}

// Match returns all matching tags, deduplicated and sorted by name, or nil if none match.
// Path must be a normalized snapshot-relative, slash-separated path; directory
// paths have no trailing slash and the root is ".". Match does not normalize paths.
// The returned slice belongs to the caller. Tags are not inherited from ancestors.
func (m *TagMatcher) Match(path string) []Tag {
	var tags []Tag
	seen := map[Tag]bool{}
	for _, rule := range m.rules {
		if !seen[rule.name] && rule.pattern.MatchString(path) {
			seen[rule.name] = true
			tags = append(tags, rule.name)
		}
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	return tags
}

// DocumentTags combines format classification with direct path tags without parsing
// content. Gitlinks remain opaque and receive only path tags, even at source paths.
// Empty TagRules disables path rules, not format classification.
func (m *TagMatcher) DocumentTags(d Document) []Tag {
	tags := m.Match(d.Path)
	if d.Gitlink == "" {
		if tag := Tag(language.MaterialTag(d.Path)); tag != "" && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
			slices.Sort(tags)
		}
	}
	return tags
}

// ExcludedBy returns the first matching exclusion tag on the document or an
// ancestor directory, or empty when admitted. It does not inherit directory tags
// into DocumentTags. Document paths must be normalized snapshot-relative paths.
// +spec=Consumers and Builder apply the same material exclusions before parsing and document budgets.
func (m *TagMatcher) ExcludedBy(d Document, excluded []Tag) Tag {
	if len(excluded) == 0 {
		return ""
	}
	for _, tag := range m.DocumentTags(d) {
		if slices.Contains(excluded, tag) {
			return tag
		}
	}
	for current := path.Dir(d.Path); ; current = path.Dir(current) {
		for _, tag := range m.Match(current) {
			if slices.Contains(excluded, tag) {
				return tag
			}
		}
		if current == "." {
			return ""
		}
	}
}
