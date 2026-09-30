package codegraph

import (
	"github.com/compforge/codegraph/internal/model"
)

// NodeKind is the concrete code category used both by Node.Kind and Cypher labels.
// +spec=`Each node has exactly its concrete kind as a graph label; symbol is terminology, not a graph category`
type NodeKind = model.NodeKind

// Location uses zero-based byte offsets (end exclusive) and one-based lines/columns.
// Columns count bytes, not Unicode code points.
type Location = model.Location

// Node is an entity in one snapshot, labeled by its concrete code kind.
// Symbol and Namespace describe overlapping roles of that same entity.
// Location is absent when no single source occurrence owns the entity;
// declares relations retain its contributing source locations.
type Node = model.Node

type RelationKind = model.RelationKind

// Confidence describes evidence strength, not a calibrated probability.
type Confidence = model.Confidence

// Evidence records one derivation. Location, when present, points to supporting syntax.
type Evidence = model.Evidence

// Relation identifies one relation at one source location, including parallel calls.
type Relation = model.Relation

// Path lists nodes in traversal order; Relations retain their stored direction.
type Path = model.Path

type Subgraph = model.Subgraph

// DiagnosticSubject identifies the information a local gap concerns. It does
// not prescribe whether a consumer should expand its analysis or reject a path.
type DiagnosticSubject = model.DiagnosticSubject

// Diagnostic records information that could not be produced. Non-exact edges
// already carry their uncertainty in Confidence and Basis.
// Location covers the whole document when the producer cannot localize the gap.
type Diagnostic = model.Diagnostic

// OutlineCoverage counts query candidates, not all declarations in the source.
// Zero omissions do not prove language coverage. The upstream outline API has
// no omission locations, so these counts apply to the diagnostic's document.
type OutlineCoverage = model.OutlineCoverage

type MarkerKind = model.MarkerKind

// Marker preserves the annotation payload and its source location.
// Structured case payloads are retained verbatim, not executed or interpreted.
type Marker = model.Marker

// BuildReport describes the last successful publication and local gaps.
type BuildReport = model.BuildReport

const (
	// DocumentKind represents the input material itself, including opaque gitlinks.
	DocumentKind = model.DocumentKind
	Struct       = model.Struct
	Interface    = model.Interface
	Field        = model.Field
	Method       = model.Method
	Function     = model.Function
	// Type represents a named type whose declaration is not a struct or interface literal.
	Type                = model.Type
	TypeAlias           = model.TypeAlias
	Class               = model.Class
	Constructor         = model.Constructor
	Variable            = model.Variable
	Constant            = model.Constant
	Module              = model.Module
	Package             = model.Package
	Enum                = model.Enum
	Record              = model.Record
	Namespace           = model.Namespace
	Property            = model.Property
	Trait               = model.Trait
	Macro               = model.Macro
	Union               = model.Union
	Contains            = model.Contains
	Declares            = model.Declares
	Imports             = model.Imports
	Calls               = model.Calls
	References          = model.References
	Extends             = model.Extends
	Implements          = model.Implements
	Exact               = model.Exact
	Scoped              = model.Scoped
	NameOnly            = model.NameOnly
	Heuristic           = model.Heuristic
	DocumentSubject     = model.DocumentSubject
	DeclarationsSubject = model.DeclarationsSubject
	RelationsSubject    = model.RelationsSubject
	ContextSubject      = model.ContextSubject
	ResourcesSubject    = model.ResourcesSubject
	Spec                = model.Spec
	Case                = model.Case
	Rule                = model.Rule
	Link                = model.Link
	Doc                 = model.Doc
)
