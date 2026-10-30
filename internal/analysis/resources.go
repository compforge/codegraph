package analysis

import "context"

// BindResourceReferences registers caller-derived path facts in the ordinary
// semantic index before it becomes immutable for selection and publication.
func (index *Index) BindResourceReferences(ctx context.Context, names []string) ([]Gap, error) {
	var issues []Gap
	for _, name := range names {
		f := index.Files[name]
		for _, ref := range f.ResourceReferences {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			target := DocumentRef(ref.Path)
			_, exists := index.Entities[target]
			if ref.Path == "" || !exists || index.Files[ref.Path].Gitlink != "" {
				code, detail := "unresolved_resource", "resource path is not statically anchored: "+ref.Expression
				if ref.Path != "" {
					code, detail = "resource_not_loaded", "resource target is not a supplied file Document: "+ref.Path
				}
				issues = append(issues, Gap{Code: code, Reference: detail, Relation: "references", Path: f.Path, Span: ref.Span})
				continue
			}
			index.Add(Edge{Source: SourceRef(f.Path, ref.Owner), Target: target, Kind: "references", Path: f.Path, Span: ref.Span, Confidence: Exact, Basis: ref.Basis})
			if err := index.Err(); err != nil {
				return nil, err
			}
		}
	}
	return issues, nil
}
