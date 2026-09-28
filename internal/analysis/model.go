package analysis

import (
	"context"
	"errors"
	"sort"
)

var ErrEdgeLimit = errors.New("relation limit reached")

// Ref is a tagged endpoint. Construction cannot confuse a document with declaration zero.
type refKind uint8

const (
	invalidEntity refKind = iota
	documentEntity
	declarationEntity
	organizationEntity
)

type Ref struct {
	Path        string
	Declaration int
	key         string
	kind        refKind
}

func DocumentRef(path string) Ref { return Ref{Path: path, Declaration: -1, kind: documentEntity} }
func DeclarationRef(path string, index int) Ref {
	return Ref{Path: path, Declaration: index, kind: declarationEntity}
}
func OrganizationRef(key string) Ref { return Ref{key: key, kind: organizationEntity} }
func SourceRef(path string, owner int) Ref {
	if owner < 0 {
		return DocumentRef(path)
	}
	return DeclarationRef(path, owner)
}
func (r Ref) IsDeclaration() bool  { return r.kind == declarationEntity }
func (r Ref) IsDocument() bool     { return r.kind == documentEntity }
func (r Ref) Organization() string { return r.key }

type Edge struct {
	Source, Target          Ref
	Kind, Confidence, Basis string
	Path                    string
	Span                    Span
}
type Gap struct {
	Path, Code, Reference, Relation string
	Span                            Span
}
type Organization struct {
	Key, Kind, Name, QualifiedName, Language string
	Documents                                []string
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
	return Scope{files, names, module}
}

// Index owns source provenance and direct membership for both declared and assembled entities.
// +spec=`Every published contains edge contributes to the same direct-member lookup`
type Index struct {
	Files      map[string]Facts
	Units      map[string]Organization
	ByDocument map[string]string
	Members    map[Ref]map[string][]Ref
	Edges      []Edge
}

func NewIndex(files map[string]Facts) *Index {
	return &Index{Files: files, Units: map[string]Organization{}, ByDocument: map[string]string{}, Members: map[Ref]map[string][]Ref{}}
}
func (x *Index) Add(e Edge) {
	x.Edges = append(x.Edges, e)
	if e.Kind != "contains" {
		return
	}
	name := ""
	if e.Target.IsDeclaration() {
		name = x.Files[e.Target.Path].Declarations[e.Target.Declaration].Name
	} else {
		name = x.Units[e.Target.Organization()].Name
	}
	if x.Members[e.Source] == nil {
		x.Members[e.Source] = map[string][]Ref{}
	}
	for _, r := range x.Members[e.Source][name] {
		if r == e.Target {
			return
		}
	}
	x.Members[e.Source][name] = append(x.Members[e.Source][name], e.Target)
}
func (x *Index) ModuleRef(p string) Ref {
	if key := x.ByDocument[p]; key != "" {
		return OrganizationRef(key)
	}
	return DocumentRef(p)
}
func (x *Index) Register(units []Organization, edges []Edge) {
	for _, u := range units {
		x.Units[u.Key] = u
		for _, p := range u.Documents {
			x.ByDocument[p] = u.Key
		}
	}
	for _, e := range edges {
		x.Add(e)
	}
}

// AddDeclarations records source ownership independently from semantic membership.
func (x *Index) AddDeclarations(ctx context.Context, names []string) error {
	for _, p := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := x.Files[p]
		for i, d := range f.Declarations {
			target := DeclarationRef(p, i)
			x.Add(Edge{Source: DocumentRef(p), Target: target, Kind: "declares", Confidence: "exact", Basis: "source_declaration", Path: p, Span: d.Span})
			if d.Parent >= 0 {
				x.Add(Edge{Source: DeclarationRef(p, d.Parent), Target: target, Kind: "contains", Confidence: "exact", Basis: "declaration", Path: p, Span: d.Span})
			} else if key := x.ByDocument[p]; key != "" && d.Receiver == "" {
				x.Add(Edge{Source: OrganizationRef(key), Target: target, Kind: "contains", Confidence: "exact", Basis: "namespace_member", Path: p, Span: d.Span})
			}
		}
	}
	return nil
}
func (x *Index) PackageCount(paths []string) int {
	seen := map[string]bool{}
	for _, p := range paths {
		seen[x.ByDocument[p]] = true
	}
	return len(seen)
}
func EnclosingDeclaration(f Facts, span Span) int {
	owner, size := -1, len(f.Source)+1
	for i, d := range f.Declarations {
		if d.Start <= span.Start && d.End >= span.End && d.End-d.Start < size {
			owner, size = i, d.End-d.Start
		}
	}
	return owner
}

type BindingTarget struct {
	Ref
	Confidence string
}
