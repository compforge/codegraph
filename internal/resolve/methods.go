package resolve

import (
	"context"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/extract"
)

// methodIndex is a per-resolution view of declarations and already-bound bases.
// It follows extends only: an implements candidate does not imply inheritance.
type methodIndex struct {
	names   []string
	bases   map[Ref][]Ref
	members map[Ref]map[string][]Ref
}

func newMethodIndex(ctx context.Context, files map[string]extract.Facts, names []string, types []Edge) (*methodIndex, error) {
	index := &methodIndex{names: names, bases: map[Ref][]Ref{}, members: map[Ref]map[string][]Ref{}}
	for _, edge := range types {
		if edge.Kind == "extends" {
			index.bases[edge.Source] = append(index.bases[edge.Source], edge.Target)
		}
	}
	receivers := receiverIndex(files, names)
	for _, p := range names {
		f := files[p]
		for i, d := range f.Declarations {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if d.Kind != "method" {
				continue
			}
			var owners []Ref
			if d.Parent >= 0 {
				owners = append(owners, Ref{p, d.Parent})
			}
			if d.Receiver != "" {
				owners = append(owners, receivers[receiverKey{path.Dir(p), f.Package, d.Receiver}]...)
			}
			for _, owner := range owners {
				if index.members[owner] == nil {
					index.members[owner] = map[string][]Ref{}
				}
				index.members[owner][d.Name] = append(index.members[owner][d.Name], Ref{p, i})
			}
		}
	}
	return index, nil
}

type methodTarget struct {
	Ref
	Inherited bool
}

// +why=`Direct methods stop lookup on that inheritance branch; multiple base branches remain candidates without runtime MRO proof`
func (index *methodIndex) lookup(ctx context.Context, roots []Ref, name string, includeTests bool, limit int) ([]methodTarget, error) {
	type visit struct {
		owner     Ref
		inherited bool
	}
	queue := make([]visit, 0, len(roots))
	for _, root := range roots {
		queue = append(queue, visit{owner: root})
	}
	seen := map[Ref]bool{}
	targets := map[Ref]int{}
	var out []methodTarget
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := queue[head]
		if seen[item.owner] {
			continue
		}
		seen[item.owner] = true

		found := false
		for _, target := range index.members[item.owner][name] {
			if !includeTests && strings.HasSuffix(target.Path, "_test.go") {
				continue
			}
			found = true
			if i, ok := targets[target]; ok {
				out[i].Inherited = out[i].Inherited && item.inherited
				continue
			}
			if len(out) >= limit {
				return nil, ErrEdgeLimit
			}
			targets[target] = len(out)
			out = append(out, methodTarget{target, item.inherited})
		}
		if found {
			continue
		}

		for _, base := range index.bases[item.owner] {
			queue = append(queue, visit{base, true})
		}
	}
	return out, nil
}
