package analysis

// Namespace is an entity's logical member-organizing view, not a node kind or
// a second entity. The same view works for packages, modules and declarations.
// +spec=`Namespace membership comes only from contains; lexical visibility remains language-owned`
type Namespace struct {
	Owner Ref
	index *Index
}

func (x *Index) Namespace(owner Ref) Namespace { return Namespace{Owner: owner, index: x} }
func (n Namespace) Members(name string) []Ref  { return n.index.members[n.Owner][name] }

// Contributions preserves each source location, including repeated contributions
// from one document. It does not infer provenance from membership descendants.
func (n Namespace) Contributions() []Edge {
	var out []Edge
	for _, i := range n.index.contributions[n.Owner] {
		out = append(out, n.index.Edges[i])
	}
	return out
}
