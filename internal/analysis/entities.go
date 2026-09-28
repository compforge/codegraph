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
	Comments                            []Comment
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
type Scope struct {
	Files  map[string]Facts
	Names  []string
	Module string
}

func NewScope(files map[string]Facts, module string) Scope {
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	return Scope{Files: files, Names: names, Module: module}
}

// Index owns all entities and relation evidence for one build. Namespace views
// and source contributions are projections of these same relations.
type Index struct {
	Files         map[string]Facts
	Entities      map[Ref]Entity
	Roots         map[string]Ref
	Edges         []Edge
	members       map[Ref]map[string][]Ref
	contributions map[Ref][]Edge
}

func NewIndex(files map[string]Facts) *Index {
	return &Index{Files: files, Entities: map[Ref]Entity{}, Roots: map[string]Ref{}, members: map[Ref]map[string][]Ref{}, contributions: map[Ref][]Edge{}}
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
	x.Edges = append(x.Edges, e)
	switch e.Kind {
	case "declares":
		x.contributions[e.Target] = append(x.contributions[e.Target], e)
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
		x.Entities[ref] = Entity{Ref: ref, Kind: "Document", Name: path.Base(p), Language: f.Language, Location: &SourceLocation{Path: p, Span: Span{End: len(f.Source)}}}
		for i, d := range f.Declarations {
			kind := ConcreteKind(d.Kind)
			if kind == "" {
				return fmt.Errorf("%s: unsupported declaration kind %q", p, d.Kind)
			}
			ref := DeclarationRef(p, i)
			x.Entities[ref] = Entity{Ref: ref, Kind: kind, Name: d.Name, QualifiedName: d.QualifiedName, Language: f.Language, Location: &SourceLocation{Path: p, Span: d.Span}, Comments: d.Comments}
			x.Add(Edge{Source: DocumentRef(p), Target: ref, Kind: "declares", Confidence: "exact", Basis: "source_declaration", Path: p, Span: d.Span})
		}
	}
	return nil
}

// AttachDeclarations uses the adapter's semantic root and declared member owners.
// +why=`Source provenance cannot substitute for an absent semantic owner`
func (x *Index) AttachDeclarations(ctx context.Context, names []string) error {
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, d := range x.Files[p].Declarations {
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
