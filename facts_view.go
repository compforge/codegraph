package codegraph

import "fmt"

// View returns a fresh detached projection of the original extraction artifact.
func (f Facts) View() (Facts, error) {
	if f.raw == nil {
		return Facts{}, fmt.Errorf("facts must be produced by an Extractor")
	}
	return projectFacts(*f.raw)
}

// Digest identifies source bytes and material kind/version, independently of
// a graph snapshot. The logical path is available in View and is part of cache identity.
func (f Facts) Digest() [32]byte {
	if f.raw == nil {
		return [32]byte{}
	}
	return (Document{Path: f.raw.Path, Content: f.raw.Source, Gitlink: f.raw.Gitlink}).digest()
}
