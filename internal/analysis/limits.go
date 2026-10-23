package analysis

import "fmt"

type Limits struct{ Entities, Relations, Evidence int }
type LimitError struct {
	Resource            string
	Used, Adding, Limit int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("analysis %s limit: used=%d adding=%d limit=%d", e.Resource, e.Used, e.Adding, e.Limit)
}
func NewBoundedIndex(files map[string]Facts, limits Limits) *Index {
	x := NewIndex(files)
	x.limits = limits
	return x
}
func (x *Index) Err() error { return x.err }
func (x *Index) reserve(resource string, used, adding, limit int) bool {
	if x.err != nil {
		return false
	}
	if limit > 0 && adding > limit-used {
		x.err = &LimitError{resource, used, adding, limit}
		return false
	}
	return true
}
func (x *Index) registerEntity(e Entity) {
	if _, exists := x.Entities[e.Ref]; !exists && !x.reserve("Entities", len(x.Entities), 1, x.limits.Entities) {
		return
	}
	if x.err == nil {
		x.Entities[e.Ref] = e
	}
}

var ErrEntityLimit = fmt.Errorf("entity limit reached")

func (e *LimitError) Unwrap() error {
	switch e.Resource {
	case "Entities":
		return ErrEntityLimit
	case "Relations":
		return ErrEdgeLimit
	case "Evidence":
		return ErrEvidenceLimit
	}
	return nil
}
