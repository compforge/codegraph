package analysis

func UniqueBindingTargets(targets []BindingTarget) []BindingTarget {
	out := make([]BindingTarget, 0, len(targets))
	indexes := map[Ref]int{}
	for _, target := range targets {
		if i, ok := indexes[target.Ref]; ok {
			if target.Confidence == "candidate" {
				out[i].Confidence = "candidate"
			}
		} else {
			indexes[target.Ref] = len(out)
			out = append(out, target)
		}
	}
	if len(out) > 1 {
		for i := range out {
			out[i].Confidence = "candidate"
		}
	}
	return out
}
