package analysis

import (
	"context"
	"fmt"
	"path"
	"sort"
)

// Entity describes one graph endpoint. Identity provenance is independent of
// its concrete kind and of its role as a symbol or namespace.
type Entity struct {
	Ref                                 Ref
	Kind, Name, QualifiedName, Language string
	Location                            *SourceLocation
	NameLocation                        *SourceLocation
	Comments                            []Comment
	Documentation                       []Documentation
}
type SourceLocation struct {
	Path string
	Span
}

// Organization supplies entities, document-level semantic owners, and evidence.
// Roots may refer to either source declarations or synthetic entities.
type Organization struct {
	Entities []Entity
	Roots    map[string]Ref
	Edges    []Edge
}
type BuildScope struct {
	Resolution ResolutionContext
	Files      map[string]Facts
	Names      []string
	Module     string
}

func NewBuildScope(files map[string]Facts, module string) BuildScope {
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	return BuildScope{Files: files, Names: names, Module: module}
}

// Index owns all entities and relation evidence for one build. Namespace views
// and source contributions are projections of these same relations.
type Index struct {
	Resolution    ResolutionContext
	Files         map[string]Facts
	Entities      map[Ref]Entity
	Roots         map[string]Ref
	Gitlinks      map[string]Ref
	Edges         []Edge
	edgeIDs       map[edgeKey]int
	EvidenceCount int
	members       map[Ref]map[string][]Ref
	contributions map[Ref][]int
}

func NewIndex(files map[string]Facts) *Index {
	return &Index{edgeIDs: map[edgeKey]int{}, Files: files, Entities: map[Ref]Entity{}, Roots: map[string]Ref{}, Gitlinks: map[string]Ref{}, members: map[Ref]map[string][]Ref{}, contributions: map[Ref][]int{}}
}

// RegisterEntities records organization endpoints and roots. The pipeline adds
// edges after every language has registered, so forward membership targets exist.
func (x *Index) RegisterEntities(o Organization) {
	for _, e := range o.Entities {
		x.Entities[e.Ref] = e
	}
	for p, root := range o.Roots {
		x.Roots[p] = root
	}
}
func (x *Index) Add(e Edge) {
	if !x.registerEdge(e) {
		return
	}
	e = x.Edges[len(x.Edges)-1]
	switch e.Kind {
	case "declares":
		x.contributions[e.Target] = append(x.contributions[e.Target], len(x.Edges)-1)
	case "contains":
		name := x.Entities[e.Target].Name
		if x.members[e.Source] == nil {
			x.members[e.Source] = map[string][]Ref{}
		}
		for _, ref := range x.members[e.Source][name] {
			if ref == e.Target {
				return
			}
		}
		x.members[e.Source][name] = append(x.members[e.Source][name], e.Target)
	}
}

// AddSources registers source entities before any organization or membership.
func (x *Index) AddSources(ctx context.Context, names []string) error {
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := x.Files[p]
		ref := DocumentRef(p)
		if f.Gitlink != "" {
			x.Gitlinks[p] = ref
		}
		x.Entities[ref] = Entity{Ref: ref, Kind: "Document", Name: path.Base(p), Language: f.Language, Location: &SourceLocation{Path: p, Span: Span{End: len(f.Source)}}}
		for i, d := range f.Declarations {
			kind := ConcreteKind(d.Kind)
			if kind == "" {
				return fmt.Errorf("%s: unsupported declaration kind %q", p, d.Kind)
			}
			ref := DeclarationRef(p, i)
			e := Entity{Ref: ref, Kind: kind, Name: d.Name, QualifiedName: d.QualifiedName, Language: f.Language, Location: &SourceLocation{Path: p, Span: d.Span}, Comments: d.Comments, Documentation: d.Documentation}
			if d.NameSpan.End > d.NameSpan.Start {
				e.NameLocation = &SourceLocation{Path: p, Span: d.NameSpan}
			}
			x.Entities[ref] = e
			x.Add(Edge{Source: DocumentRef(p), Target: ref, Kind: "declares", Confidence: "exact", Basis: "source_declaration", Path: p, Span: d.Span})
		}
	}
	return nil
}

// AttachDeclarations records lexical nesting independently of semantic ownership.
// +spec=Every retained declaration has one encloses parent in its own document; binding never reparents that lexical edge.
// +why=`Source provenance cannot substitute for an absent semantic owner`
func (x *Index) AttachDeclarations(ctx context.Context, names []string) error {
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, d := range x.Files[p].Declarations {
			lexical := DocumentRef(p)
			if d.Parent >= 0 {
				lexical = DeclarationRef(p, d.Parent)
			}
			x.Add(Edge{Source: lexical, Target: DeclarationRef(p, i), Kind: "encloses", Confidence: "exact", Basis: "lexical_nesting", Path: p, Span: d.Span})
			owner, ok := x.Roots[p]
			basis := "namespace_member"
			if d.Parent >= 0 {
				owner, ok = DeclarationRef(p, d.Parent), true
				basis = "declaration"
			} else if d.Receiver != "" {
				continue
			}
			target := DeclarationRef(p, i)
			if ok && owner != target {
				x.Add(Edge{Source: owner, Target: target, Kind: "contains", Confidence: "exact", Basis: basis, Path: p, Span: d.Span})
			}
		}
	}
	return nil
}
