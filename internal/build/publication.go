package build

import (
	"sort"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/graphmodel"
	"github.com/compforge/codegraph/internal/graphstore"
)

func (g *Builder) allowed(p string) bool {
	for _, s := range g.opts.Scope {
		if s == "." || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return len(g.opts.Scope) == 0
}
func (g *Builder) allowedDir(p string) bool { return g.allowed(p) }

func sortedFiles(files map[string]analysis.Facts) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func newGraph(snapshot string, opts Options, nodes map[string]graphmodel.Node, relations map[string]graphmodel.Relation, report graphmodel.BuildReport) *graphstore.Snapshot {
	return graphstore.NewSnapshot(snapshot, nodes, relations, report, graphstore.Limits{Rows: opts.MaxResultRows, Bytes: opts.MaxResultBytes, Hops: opts.MaxQueryHops}, opts.QueryTimeout)
}
