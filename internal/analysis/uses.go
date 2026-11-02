package analysis

import "github.com/compforge/codegraph/pkg"

// These kinds belong to binding and lowering, not the public graph vocabulary.
// They share the open enum with public operations; package boundaries establish
// visibility, never capitalization of serialized values.
const (
	CompositeKeyUse pkg.RelationKind = "composite_key_use"
	NameUse         pkg.RelationKind = "name_use"
	CallTargetUse   pkg.RelationKind = "call_target_use"

	ReadWriteUse pkg.RelationKind = "read_write_use"
)

// Candidate excludes known shadow-only bindings. The
// original records remain available to call resolution and shadowing checks.
func (r Reference) Candidate(f Facts) bool {
	if r.Kind == CompositeKeyUse || r.Kind == pkg.Decorates || r.Member || r.Receiver != "" || r.BindingState() != Shadowed {
		return true
	}
	// Target=-1 can mean multiple retained lexical candidates, not only an
	// unrepresented local. Preserve those candidates and their scoped confidence.
	if f.Lexical != nil {
		bindings := f.Lexical.Lookup(r.Name, r.Span)
		if len(bindings) > 1 {
			for _, b := range bindings {
				if b.Target < 0 {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (r Reference) UseKind() pkg.RelationKind {
	if r.Kind == "" {
		return NameUse
	}
	return r.Kind
}

// Lower resolves intermediate operations only after every language has bound
// its names. Intermediate operations never enter the semantic relation index.
// +spec=Binding-only records do not consume relation or graph-object budgets.
// +link=component://docs/semantic-kinds.md
func (x *Index) Lower(e Edge) {
	kind := e.Kind
	switch kind {
	case ReadWriteUse:
		e.Kind = pkg.Reads
		x.Lower(e)
		e.Kind = pkg.Writes
		x.Lower(e)
		return
	case NameUse, CallTargetUse, CompositeKeyUse:
		target := x.Entities[e.Target].Kind
		switch target {
		case pkg.Struct, pkg.Interface, pkg.Class, pkg.Type, pkg.TypeAlias, pkg.Enum, pkg.Record, pkg.Trait, pkg.Union:
			kind = pkg.Reads
			if e.Kind == CallTargetUse || x.Files[e.Path].Language == "go" {
				kind = pkg.UsesType
			}
		default:
			kind = pkg.Reads
		}
		if e.Kind == CallTargetUse && (target == pkg.Function || target == pkg.Method || target == pkg.Constructor) {
			// Calls already establish this dependency. Unsupported dynamic calls still
			// retain a value dependency rather than silently losing a resolved target.
			for _, call := range x.callSites[e.Source] {
				if call.Target == e.Target && call.Path == e.Path && call.Span.Start <= e.Span.Start && call.Span.End >= e.Span.End {
					return
				}
			}
		}
	}
	e.Kind = kind
	x.Add(e)
	if (kind == pkg.Reads || kind == pkg.Writes) && x.isCapture(e) {
		e.Kind = pkg.Captures
		x.Add(e)
	}
}

func (x *Index) isCapture(e Edge) bool {
	if !e.Source.IsDeclaration() || !e.Target.IsDeclaration() || e.Source.Path != e.Target.Path {
		return false
	}
	f := x.Files[e.Source.Path]
	target := f.Declarations[e.Target.Declaration]
	if target.Kind != "variable" && target.Kind != "constant" {
		return false
	}
	owner := func(i int) int {
		for i >= 0 {
			d := f.Declarations[i]
			if d.Kind == "function" || d.Kind == "method" {
				return i
			}
			i = d.Parent
		}
		return -1
	}
	sourceOwner, targetOwner := owner(e.Source.Declaration), owner(target.Parent)
	if sourceOwner < 0 || targetOwner < 0 || sourceOwner == targetOwner {
		return false
	}
	for i := f.Declarations[sourceOwner].Parent; i >= 0; i = f.Declarations[i].Parent {
		if i == targetOwner {
			return true
		}
	}
	return false
}
