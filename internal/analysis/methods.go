package analysis

import "context"

type MethodIndex struct {
	index *Index
	Bases map[Ref][]BindingTarget
}

func NewMethodIndex(index *Index) *MethodIndex {
	m := &MethodIndex{index: index, Bases: map[Ref][]BindingTarget{}}
	for _, e := range index.Edges {
		if e.Kind == "extends" {
			m.Bases[e.Source] = append(m.Bases[e.Source], BindingTarget{Ref: e.Target, Confidence: e.Confidence})
		}
	}
	return m
}

type MethodTarget struct {
	BindingTarget
	Inherited bool
}

// +why=`Direct methods stop lookup on that inheritance branch; multiple base branches remain candidates without runtime MRO proof`
func (index *MethodIndex) Lookup(ctx context.Context, roots []BindingTarget, name string, eligible func(Ref) bool, limit int) ([]MethodTarget, error) {
	type visit struct {
		owner     BindingTarget
		inherited bool
	}
	queue := make([]visit, 0, len(roots))
	for _, root := range roots {
		queue = append(queue, visit{owner: root})
	}
	seen := map[Ref]Confidence{}
	targets := map[Ref]int{}
	var out []MethodTarget
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := queue[head]
		if prior, ok := seen[item.owner.Ref]; ok && prior.AtLeast(item.owner.Confidence) {
			continue
		}
		seen[item.owner.Ref] = item.owner.Confidence

		found := false
		for _, target := range index.index.Namespace(item.owner.Ref).Members(name) {
			if !target.IsDeclaration() || index.index.Files[target.Path].Declarations[target.Declaration].Kind != "method" || !eligible(target) {
				continue
			}
			found = true
			if i, ok := targets[target]; ok {
				out[i].Inherited = out[i].Inherited && item.inherited
				out[i].Confidence = out[i].Confidence.Stronger(item.owner.Confidence)
				continue
			}
			if len(out) >= limit {
				return nil, ErrEdgeLimit
			}
			targets[target] = len(out)
			out = append(out, MethodTarget{BindingTarget: BindingTarget{Ref: target, Confidence: item.owner.Confidence}, Inherited: item.inherited})
		}
		if found {
			continue
		}

		for _, base := range index.Bases[item.owner.Ref] {
			base.Confidence = base.Confidence.Weaker(item.owner.Confidence)
			queue = append(queue, visit{base, true})
		}
	}
	return out, nil
}
