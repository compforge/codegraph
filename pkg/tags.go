// Package pkg defines CodeGraph's public value model. It has no dependency on analysis or graph storage.
package pkg

// Tag is an open classification vocabulary. Applications may define additional
// values without registration; tags do not select parsers or exclusion policy.
type Tag string

const (
	UnknownTag       Tag = "unknown"
	SourceTag        Tag = "source"
	DocumentationTag Tag = "documentation"
	VersionTag       Tag = "version"
	ResourceTag      Tag = "resource"
	ToolingTag       Tag = "tooling"
	ConfigTag        Tag = "config"
	ManifestTag      Tag = "manifest"
	LockfileTag      Tag = "lockfile"
	GeneratedTag     Tag = "generated"
	TestTag          Tag = "test"
	TestFixtureTag   Tag = "test_fixture"
	DependencyTag    Tag = "dependency"
	BuildOutputTag   Tag = "build_output"
	CacheTag         Tag = "cache"
	MinifiedTag      Tag = "minified"
)
