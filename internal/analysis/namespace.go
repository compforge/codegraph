package analysis

import "context"

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

// AttachNamespaces publishes the organizer's document context as a graph fact.
// A root may be a declaration or a synthetic entity; source-location shape is
// not evidence of ownership. Language adapters determine roots, not path splitting.
func (x *Index) AttachNamespaces(ctx context.Context, names []string) error {
	// Index source spans once: many documents may contribute to the same package.
	spans := map[string]Span{}
	for _, edge := range x.Edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		if edge.Kind == "declares" && edge.Source == DocumentRef(edge.Path) && x.Roots[edge.Path] == edge.Target {
			if _, exists := spans[edge.Path]; !exists {
				spans[edge.Path] = edge.Span
			}
		}
	}
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, ok := x.Roots[p]
		if !ok {
			continue
		}
		span, located := spans[p]
		if !located {
			span = Span{End: len(x.Files[p].Source)}
		}
		x.Add(Edge{Source: DocumentRef(p), Target: root, Kind: "in_namespace", Path: p, Span: span, Confidence: Exact, Basis: "document_namespace"})
	}
	return nil
}
