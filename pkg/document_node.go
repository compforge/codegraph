// Package pkg defines CodeGraph's public value model. It has no dependency on analysis or graph storage.
package pkg

// ManifestMetadata preserves explicit project declarations. Name is a Go module
// path or a packaging project name; Python/JS names are not import namespaces.
// Absent metadata does not imply that a project lacks a dynamically supplied value.
type ManifestMetadata struct {
	Format          string    `json:"format"`
	Name            string    `json:"name,omitempty"`
	Version         string    `json:"version,omitempty"`
	Project         bool      `json:"project"`
	BuildSystem     bool      `json:"buildSystem"`
	Workspace       bool      `json:"workspace"`
	NameLocation    *Location `json:"nameLocation,omitempty"`
	VersionLocation *Location `json:"versionLocation,omitempty"`
}
