package analysis

// Manifest is detached, immutable evidence from a supplied project manifest.
// Package-manager execution and repository Component decisions are consumer-owned.
type Manifest struct {
	Format, Name, Version           string
	Project, BuildSystem, Workspace bool
	NameSpan, VersionSpan           Span
}
