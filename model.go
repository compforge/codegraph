package codegraph

import (
	"github.com/compforge/codegraph/internal/graphmodel"
)

// NodeKind is the concrete code category used both by Node.Kind and Cypher labels.
// +spec=`Each node has exactly its concrete kind as a graph label; symbol is terminology, not a graph category`
type NodeKind = graphmodel.NodeKind

// Location uses zero-based byte offsets (end exclusive) and one-based lines/columns.
// Columns count bytes, not Unicode code points.
type Location = graphmodel.Location

// Node is an entity in one snapshot, labeled by its concrete code kind.
// Symbol and Namespace describe overlapping roles of that same entity.
// Location is absent when no single source occurrence owns the entity;
// declares relations retain its contributing source locations.
type Node = graphmodel.Node

type RelationKind = graphmodel.RelationKind

// Confidence describes evidence strength, not a calibrated probability.
type Confidence = graphmodel.Confidence

// Evidence records one derivation. Location, when present, points to supporting syntax.
type Evidence = graphmodel.Evidence

// Relation identifies one relation at one source location, including parallel calls.
type Relation = graphmodel.Relation

// Path lists nodes in traversal order; Relations retain their stored direction.
type Path = graphmodel.Path

type Subgraph = graphmodel.Subgraph

// DiagnosticSubject identifies the information a local gap concerns. It does
// not prescribe whether a consumer should expand its analysis or reject a path.
type DiagnosticSubject = graphmodel.DiagnosticSubject

// Diagnostic records information that could not be produced. Non-exact edges
// already carry their uncertainty in Confidence and Basis.
// Location covers the whole document when the producer cannot localize the gap.
type Diagnostic = graphmodel.Diagnostic

// OutlineCoverage counts query candidates, not all declarations in the source.
// Zero omissions do not prove language coverage. The upstream outline API has
// no omission locations, so these counts apply to the diagnostic's document.
type OutlineCoverage = graphmodel.OutlineCoverage

type MarkerKind = graphmodel.MarkerKind

// Marker preserves the annotation payload and its source location.
// Structured case payloads are retained verbatim, not executed or interpreted.
type Marker = graphmodel.Marker

// BuildReport describes the last successful publication and local gaps.
type BuildReport = graphmodel.BuildReport

const (
	// DocumentKind represents the input material itself, including opaque gitlinks.
	DocumentKind = graphmodel.DocumentKind
	Struct       = graphmodel.Struct
	Interface    = graphmodel.Interface
	Field        = graphmodel.Field
	Method       = graphmodel.Method
	Function     = graphmodel.Function
	// Type represents a named type whose declaration is not a struct or interface literal.
	Type                = graphmodel.Type
	TypeAlias           = graphmodel.TypeAlias
	Class               = graphmodel.Class
	Constructor         = graphmodel.Constructor
	Variable            = graphmodel.Variable
	Constant            = graphmodel.Constant
	Module              = graphmodel.Module
	Package             = graphmodel.Package
	Enum                = graphmodel.Enum
	Record              = graphmodel.Record
	Namespace           = graphmodel.Namespace
	Property            = graphmodel.Property
	Trait               = graphmodel.Trait
	Macro               = graphmodel.Macro
	Union               = graphmodel.Union
	Contains            = graphmodel.Contains
	Declares            = graphmodel.Declares
	Imports             = graphmodel.Imports
	Calls               = graphmodel.Calls
	References          = graphmodel.References
	Extends             = graphmodel.Extends
	Implements          = graphmodel.Implements
	Exact               = graphmodel.Exact
	Scoped              = graphmodel.Scoped
	NameOnly            = graphmodel.NameOnly
	Heuristic           = graphmodel.Heuristic
	DocumentSubject     = graphmodel.DocumentSubject
	DeclarationsSubject = graphmodel.DeclarationsSubject
	RelationsSubject    = graphmodel.RelationsSubject
	ContextSubject      = graphmodel.ContextSubject
	ResourcesSubject    = graphmodel.ResourcesSubject
	Spec                = graphmodel.Spec
	Case                = graphmodel.Case
	Rule                = graphmodel.Rule
	Link                = graphmodel.Link
	Doc                 = graphmodel.Doc
)
