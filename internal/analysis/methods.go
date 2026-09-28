package analysis

import "context"

type MethodIndex struct {
	index *Index
	Bases map[Ref][]Ref
}

func NewMethodIndex(index *Index) *MethodIndex {
	m := &MethodIndex{index: index, Bases: map[Ref][]Ref{}}
	for _, e := range index.Edges {
		if e.Kind == "extends" {
			m.Bases[e.Source] = append(m.Bases[e.Source], e.Target)
		}
	}
	return m
}

type MethodTarget struct {
	Ref
	Inherited bool
}

// +why=`Direct methods stop lookup on that inheritance branch; multiple base branches remain candidates without runtime MRO proof`
func (index *MethodIndex) Lookup(ctx context.Context, roots []Ref, name string, eligible func(Ref) bool, limit int) ([]MethodTarget, error) {
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
	var out []MethodTarget
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
		for _, target := range index.index.Members[item.owner][name] {
			if !target.IsDeclaration() || index.index.Files[target.Path].Declarations[target.Declaration].Kind != "method" || !eligible(target) {
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
			out = append(out, MethodTarget{Ref: target, Inherited: item.inherited})
		}
		if found {
			continue
		}

		for _, base := range index.Bases[item.owner] {
			queue = append(queue, visit{base, true})
		}
	}
	return out, nil
}
