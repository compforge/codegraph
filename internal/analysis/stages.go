package analysis

import (
	"context"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Extractor borrows the tree for this call only; returned facts must be detached.
type Extractor interface {
	Extract(context.Context, Facts, *gts.Tree, grammars.LangEntry) (Facts, error)
}
type Organizer interface {
	Organize(context.Context, BuildScope) (Organization, error)
}

// Binder completes names, types and receiver ownership before any resolver runs.
type Binder interface {
	Bind(context.Context, BuildScope, *Index, int) (BindResult, error)
}
type RelationResolver interface {
	Resolve(context.Context, *Index, int) ([]Edge, []Gap, error)
}
type BindResult struct {
	Edges    []Edge
	Issues   []Gap
	Resolver RelationResolver
}
type Capability struct {
	Documentation                                                bool
	Language                                                     string
	Organizations, Declarations, Relations, Markers, Limitations []string
}
type Adapter struct {
	Extractor Extractor
	Organizer Organizer
	Binder    Binder
	Describe  func(grammars.LangEntry) Capability
}
