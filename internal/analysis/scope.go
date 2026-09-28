package analysis

// Scope describes lexical visibility, independently of Namespace membership.
// Parent is the lookup parent; a language can skip a class scope for methods.
type Scope struct {
	Span
	Parent int
	Kind   string
}

// Binding keeps a name's declaration and hints in its owning lexical scope.
// Target=-1 means a binding exists without a supported graph declaration.
type Binding struct {
	Name, Kind    string
	Scope, Target int
	Span
	NameSite Span
	Hints    []CallTarget
}

// Lexicon is detached, immutable after extraction, and shared by every use pass.
type Lexicon struct {
	Scopes   []Scope
	Bindings []Binding
	names    map[int]map[string][]int
	syntax   map[Span]bool
}

func NewLexicon(size int) *Lexicon {
	return &Lexicon{Scopes: []Scope{{Span: Span{End: size}, Parent: -1, Kind: "module"}}, names: map[int]map[string][]int{}, syntax: map[Span]bool{}}
}
func (l *Lexicon) AddScope(span Span, parent int, kind string) int {
	l.Scopes = append(l.Scopes, Scope{span, parent, kind})
	return len(l.Scopes) - 1
}
func (l *Lexicon) Add(b Binding) {
	if l.names[b.Scope] == nil {
		l.names[b.Scope] = map[string][]int{}
	}
	for _, i := range l.names[b.Scope][b.Name] {
		old := l.Bindings[i]
		if old.Span == b.Span && old.NameSite == b.NameSite {
			return
		}
	}
	l.names[b.Scope][b.Name] = append(l.names[b.Scope][b.Name], len(l.Bindings))
	l.Bindings = append(l.Bindings, b)
	l.MarkSyntax(b.NameSite)
}
func (l *Lexicon) MarkSyntax(span Span)    { l.syntax[span] = true }
func (l *Lexicon) IsSyntax(span Span) bool { return l.syntax[span] }
func (l *Lexicon) ScopeAt(span Span) int {
	best := 0
	for i, s := range l.Scopes {
		b := l.Scopes[best]
		if s.Start <= span.Start && s.End >= span.End && s.End-s.Start <= b.End-b.Start {
			best = i
		}
	}
	return best
}
func (l *Lexicon) Lookup(name string, use Span) []Binding {
	for scope := l.ScopeAt(use); scope >= 0; scope = l.Scopes[scope].Parent {
		ids := l.names[scope][name]
		if len(ids) == 0 {
			continue
		}
		result := make([]Binding, 0, len(ids))
		for _, i := range ids {
			result = append(result, l.Bindings[i])
		}
		return result
	}
	return nil
}
