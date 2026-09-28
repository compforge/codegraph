package analysis

func UniqueBindingTargets(targets []BindingTarget) []BindingTarget {
	out := make([]BindingTarget, 0, len(targets))
	indexes := map[Ref]int{}
	for _, target := range targets {
		if i, ok := indexes[target.Ref]; ok {
			// Duplicate routes are independent proofs of the same binding.
			out[i].Confidence = out[i].Confidence.Stronger(target.Confidence)
		} else {
			indexes[target.Ref] = len(out)
			out = append(out, target)
		}
	}
	if len(out) > 1 {
		for i := range out {
			out[i].Confidence = out[i].Confidence.Weaker(Scoped)
		}
	}
	return out
}
