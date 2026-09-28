package analysis

// MatchState separates a missing rule from a proven shadow that forbids fallback.
type MatchState uint8

const (
	NotApplicable MatchState = iota
	Unresolved
	Bound
	Candidates
	Shadowed
)

func (r Reference) BindingState() MatchState {
	if !r.Bound {
		return NotApplicable
	}
	if r.Target < 0 {
		return Shadowed
	}
	return Bound
}
func TargetState(targets []BindingTarget) MatchState {
	if len(targets) == 0 {
		return Unresolved
	}
	if len(targets) > 1 {
		return Candidates
	}
	if targets[0].Confidence == "candidate" {
		return Candidates
	}
	return Bound
}
