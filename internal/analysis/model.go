package analysis

import (
	"errors"
)

var ErrEdgeLimit = errors.New("relation limit reached")

// Identity provenance is independent of an entity's logical roles.
type refKind uint8

const (
	invalidEntity refKind = iota
	documentEntity
	declarationEntity
	syntheticEntity
)

// Ref addresses an entity by document, declaration occurrence, or language-owned
// synthetic key. None of these identity forms implies a Namespace or Symbol role.
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
func SyntheticRef(key string) Ref { return Ref{key: key, kind: syntheticEntity} }
func SourceRef(path string, owner int) Ref {
	if owner < 0 {
		return DocumentRef(path)
	}
	return DeclarationRef(path, owner)
}
func (r Ref) IsDeclaration() bool  { return r.kind == declarationEntity }
func (r Ref) IsDocument() bool     { return r.kind == documentEntity }
func (r Ref) SyntheticKey() string { return r.key }

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
