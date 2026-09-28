package analysis

import (
	"sort"
	"strings"
)

// GitlinkPaths projects language-owned import path candidates onto supplied
// opaque gitlink boundaries. It never creates child modules or guesses package names.
func (x *Index) GitlinkPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, candidate := range paths {
		for root := range x.Gitlinks {
			if candidate == root || strings.HasPrefix(candidate, root+"/") {
				seen[root] = true
			}
		}
	}
	var roots []string
	for root := range seen {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}
