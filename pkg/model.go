// Package pkg defines CodeGraph's public value model. It has no dependency on analysis or graph storage.
package pkg

import "time"

// NodeKind is the concrete code category used both by Node.Kind and Cypher labels.
// +spec=`Each node has exactly its concrete kind as a graph label; symbol is terminology, not a graph category`
type NodeKind string

const (
	// DocumentNodeKind represents the input material itself, including opaque gitlinks.
	DocumentNodeKind NodeKind = "Document"
	// DirectoryNodeKind represents an ancestor path of supplied documents.
	DirectoryNodeKind NodeKind = "Directory"
	// Deprecated: use a concrete occurrence kind. Builders no longer emit Reference.
	Reference NodeKind = "Reference"
	Import    NodeKind = "Import"
	Export    NodeKind = "Export"
	Struct    NodeKind = "Struct"
	Interface NodeKind = "Interface"
	Field     NodeKind = "Field"
	Method    NodeKind = "Method"
	Function  NodeKind = "Function"
	// Type represents a named type whose declaration is not a struct or interface
	// literal (for example, type ID int); it does not infer an underlying type.
	Type        NodeKind = "Type"
	TypeAlias   NodeKind = "TypeAlias"
	Class       NodeKind = "Class"
	Constructor NodeKind = "Constructor"
	Variable    NodeKind = "Variable"
	Constant    NodeKind = "Constant"
	Module      NodeKind = "Module"
	Package     NodeKind = "Package"
	Enum        NodeKind = "Enum"
	Record      NodeKind = "Record"
	Namespace   NodeKind = "Namespace"
	Property    NodeKind = "Property"
	Trait       NodeKind = "Trait"
	Macro       NodeKind = "Macro"
	Union       NodeKind = "Union"
)

// Location uses zero-based byte offsets (end exclusive) and one-based lines/columns.
// Columns count bytes, not Unicode code points.
type Location struct {
	Path      string `json:"path"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine"`
	EndColumn int    `json:"endColumn"`
}

// Node is an entity in one snapshot, labeled by its concrete code kind.
// Symbol and Namespace describe overlapping roles of that same entity.
// Location is absent when no single source occurrence owns the entity;
// declares relations retain its contributing source locations.
type Node struct {
	ID string `json:"id"`
	// Entrypoint identifies a runtime entrypoint recognized by supported language
	// rules. False means none was identified; it is not proof of no external callers.
	Entrypoint bool `json:"entrypoint,omitempty"`
	// Path and Tags belong to Document and Directory nodes. Tags combine material
	// classification and direct path matches; ancestor tags are not inherited.
	Path string `json:"path,omitempty"`
	Tags []Tag  `json:"tags,omitempty"`
	// Manifest is present only on Document nodes.
	Manifest *ManifestMetadata `json:"manifest,omitempty"`
	// Gitlink is set only on an opaque gitlink Document, never on symbols.
	Gitlink       string   `json:"gitlink,omitempty"`
	Kind          NodeKind `json:"kind"`
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualifiedName,omitempty"`
	// ReferenceKind describes an occurrence node's semantic operation.
	// It remains set after a target is resolved;
	// non-occurrence nodes leave it empty.
	ReferenceKind ReferenceKind `json:"referenceKind,omitempty"`
	// Binding is present on source import/export items, independently of resolution.
	Binding *ModuleBinding `json:"binding,omitempty"`
	// Resource is present on file reference occurrences, including unresolved paths.
	Resource *ResourceReference `json:"resource,omitempty"`
	// Receiver is the extractor's lexical receiver spelling on a source-use
	// node, not a resolved type or qualified target name.
	Receiver string    `json:"receiver,omitempty"`
	Language string    `json:"language"`
	Location *Location `json:"location,omitempty"`
	// NameLocation identifies the name token/capture, independently of the full
	// declaration Location. It is absent for documents, synthetic organizations,
	// or declarations whose extractor cannot provide it.
	// Signature preserves exact header text, excluding the implementation body.
	// Absence means this declaration has no supported header extraction.
	Signature         string          `json:"signature,omitempty"`
	SignatureLocation *Location       `json:"signatureLocation,omitempty"`
	NameLocation      *Location       `json:"nameLocation,omitempty"`
	Markers           []Marker        `json:"markers,omitempty"`
	Documentation     []Documentation `json:"documentation,omitempty"`
}

// ResourceReference preserves the argument spelling and a proven snapshot-relative
// path. Empty Path means the expression or its base is not statically known.
type ResourceReference struct {
	Expression string `json:"expression"`
	Path       string `json:"path,omitempty"`
}

// ReferenceKind is the source-use role, independent of whether a target exists.
// Its vocabulary is separate from the relations established by static analysis.
type ReferenceKind string

const (
	CallReference      ReferenceKind = "calls"
	SymbolReference    ReferenceKind = "references"
	BaseReference      ReferenceKind = "extends"
	InterfaceReference ReferenceKind = "implements"
	DecoratorReference ReferenceKind = "decorates"
)

// ModuleBinding preserves a source import/export item. Empty local/exported names
// are meaningful for side-effect imports and wildcard exports. Specifier is source
// spelling, not a resolved path. Form is named, default, namespace, wildcard,
// side_effect or dynamic. Dynamic specifiers retain unevaluated source text.
type ModuleBinding struct {
	Specifier    string `json:"specifier,omitempty"`
	ImportedName string `json:"importedName,omitempty"`
	LocalName    string `json:"localName,omitempty"`
	ExportedName string `json:"exportedName,omitempty"`
	Form         string `json:"form"`
	TypeOnly     bool   `json:"typeOnly,omitempty"`
}

type RelationKind string

const (
	// InDirectory connects a Document or Directory to its direct parent Directory.
	// It is path structure, independent of language namespace membership.
	InDirectory RelationKind = "in_directory"
	// InNamespace connects a Document to its language-defined organization root.
	// It is independent of source contributions and does not make documents members.
	InNamespace RelationKind = "in_namespace"
	// Encloses is direct lexical nesting among retained declarations in one
	// document. Top-level declarations are enclosed by the Document itself.
	Encloses RelationKind = "encloses"
	// Contains records semantic membership, including cross-file owners.
	Contains RelationKind = "contains"
	Declares RelationKind = "declares"
	Imports  RelationKind = "imports"
	Calls    RelationKind = "calls"
	// Deprecated: use a concrete semantic dependency kind. Builders never emit References.
	References RelationKind = "references"
	Extends    RelationKind = "extends"
	Implements RelationKind = "implements"
	// OccursIn connects a source-use node to its nearest retained declaration,
	// or its Document when no declaration encloses the use.
	OccursIn RelationKind = "occurs_in"
	// Aliases connects a source name binding to the entity it denotes.
	Aliases RelationKind = "aliases"
	Exports RelationKind = "exports"
	// Decorates connects a modifier occurrence to the declaration it modifies.
	Decorates RelationKind = "decorates"
)

// Evidence records one derivation. Location, when present, points to supporting syntax.
type Evidence struct {
	Basis      string     `json:"basis"`
	Confidence Confidence `json:"confidence"`
	Location   *Location  `json:"location,omitempty"`
}

// Relation identifies one relation at one source location, including parallel calls.
type Relation struct {
	ID     string       `json:"id"`
	Source string       `json:"source"`
	Target string       `json:"target"`
	Kind   RelationKind `json:"kind"`
	// Confidence is the strongest independent Evidence confidence when published.
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
	Location   Location   `json:"location"`
}

// Path lists nodes in traversal order; Relations retain their stored direction.
type Path struct {
	Nodes     []Node     `json:"nodes"`
	Relations []Relation `json:"relations"`
}

type Subgraph struct {
	Nodes     []Node     `json:"nodes"`
	Relations []Relation `json:"relations"`
}

// DiagnosticSubject identifies the information a local gap concerns. It does
// not prescribe whether a consumer should expand its analysis or reject a path.
type DiagnosticSubject string

const (
	DocumentSubject     DiagnosticSubject = "document"
	DeclarationsSubject DiagnosticSubject = "declarations"
	RelationsSubject    DiagnosticSubject = "relations"
	ContextSubject      DiagnosticSubject = "context"
	ResourcesSubject    DiagnosticSubject = "resources"
)

// Diagnostic records information that could not be produced. Non-exact edges
// already carry their uncertainty in Confidence and Basis.
// Location covers the whole document when the producer cannot localize the gap.
type Diagnostic struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Subject  DiagnosticSubject `json:"subject"`
	Relation RelationKind      `json:"relation,omitempty"`
	Location Location          `json:"location"`
	Outline  *OutlineCoverage  `json:"outline,omitempty"`
}

// OutlineCoverage counts query candidates, not all declarations in the source.
// Zero omissions do not prove language coverage. The upstream outline API has
// no omission locations, so these counts apply to the diagnostic's document.
type OutlineCoverage struct {
	Symbols                    int    `json:"symbols"`
	OmittedNoName              int    `json:"omittedNoName,omitempty"`
	OmittedDuplicate           int    `json:"omittedDuplicate,omitempty"`
	OmittedNameConflict        int    `json:"omittedNameConflict,omitempty"`
	OmittedConflict            int    `json:"omittedConflict,omitempty"`
	OmittedOverlap             int    `json:"omittedOverlap,omitempty"`
	OmittedInvalidNameRange    int    `json:"omittedInvalidNameRange,omitempty"`
	OmittedMultipleDefinitions int    `json:"omittedMultipleDefinitions,omitempty"`
	OwnerRuleMisses            int    `json:"ownerRuleMisses,omitempty"`
	DeclineReason              string `json:"declineReason,omitempty"`
	Truncated                  bool   `json:"truncated,omitempty"`
}

// Documentation preserves a declaration's ordinary documentation as exact source.
// Text equals the bytes at Location, including comment delimiters or string
// prefixes/quotes. It is neither unescaped nor summarized. Fragments are ordered
// by source position; an empty slice means no documentation was extracted under
// the language adapter's supported rules (see Capabilities).
// +spec=Documentation retains source identity and never substitutes for explicit intent markers.
type Documentation struct {
	Text     string   `json:"text"`
	Location Location `json:"location"`
}

type MarkerKind string

const (
	Spec MarkerKind = "spec"
	Case MarkerKind = "case"
	Rule MarkerKind = "rule"
	Link MarkerKind = "link"
	Doc  MarkerKind = "doc"
)

// Marker preserves the annotation payload and its source location.
// Structured case payloads are retained verbatim, not executed or interpreted.
type Marker struct {
	Kind     MarkerKind `json:"kind"`
	Text     string     `json:"text"`
	Location Location   `json:"location"`
}

// BuildReport describes the published graph and its local information gaps.
// Build/Wait errors describe execution failures; diagnostics do not invalidate
// unrelated graph facts or prescribe a consumer's fallback policy.
// +spec=`Candidate relations and local gaps remain usable analysis results`
type BuildReport struct {
	Snapshot         string       `json:"snapshot"`
	Documents        []string     `json:"documents"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
	Nodes, Relations int
	Materials        MaterialReport    `json:"materials"`
	Analysis         AnalysisReport    `json:"analysis"`
	Selection        SelectionReport   `json:"selection"`
	Publication      PublicationReport `json:"publication"`
}

// Stage reports distinguish semantic work from the public object count.
// MaterialReport counts current document outcomes; scope and policy rejections are excluded.
// +spec=Submitted equals Accepted plus Excluded plus Failed.
type MaterialReport struct{ Submitted, Accepted, Excluded, Failed int }

type AnalysisReport struct {
	Entities, RawUses, Uses, Relations, Evidence int
	Duration                                     time.Duration
}

type SelectionReport struct {
	Core, Bridges, Context, Steps int
	Duration                      time.Duration
}

type PublicationReport struct {
	References int
	Duration   time.Duration
}
