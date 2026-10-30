// Package nodejs supplies opt-in platform semantics for CodeGraph.
// It declares API identities; parsing, binding and graph publication stay in core.
package nodejs

import cg "github.com/compforge/codegraph"

// ResourceReferences describes global and imported URL constructors available in
// Node.js. A URL reference is not evidence that the file is actually read.
func ResourceReferences() []cg.ResourceReferenceRule {
	rules := make([]cg.ResourceReferenceRule, 0, 3)
	for _, module := range []string{"", "node:url", "url"} {
		rules = append(rules, cg.ResourceReferenceRule{Callable: cg.Callable{Module: module, Name: "URL"}, Constructor: true, PathArgument: 0, BaseArgument: 1, Basis: "nodejs_file_url"})
	}
	return rules
}
