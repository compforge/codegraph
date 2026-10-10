package codegraph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/confidence"
)

// NodeKind is the concrete code category used both by Node.Kind and Cypher labels.
// +spec=`Each node has exactly its concrete kind as a graph label; symbol is terminology, not a graph category`
type NodeKind string

const (
	// DocumentNodeKind represents the input material itself, including opaque gitlinks.
	DocumentNodeKind NodeKind = "Document"
	// DirectoryNodeKind represents an ancestor path of supplied documents.
	DirectoryNodeKind NodeKind = "Directory"
	// Reference identifies a source use independently of whether static analysis
	// can bind a target. Node.ReferenceKind distinguishes calls from other uses.
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
	// Path and Tags belong to Document and Directory nodes. Tags are direct
	// path matches; they are neither inherited nor parsing instructions.
	Path string `json:"path,omitempty"`
	Tags []Tag  `json:"tags,omitempty"`
	// DocumentKind and Manifest are present only on Document nodes.
	DocumentKind DocumentKind      `json:"documentKind,omitempty"`
	Manifest     *ManifestMetadata `json:"manifest,omitempty"`
	// Gitlink is set only on an opaque gitlink Document, never on symbols.
	Gitlink       string   `json:"gitlink,omitempty"`
	Kind          NodeKind `json:"kind"`
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualifiedName,omitempty"`
	// ReferenceKind describes a Reference node's syntactic use.
	// It remains set after a target is resolved;
	// non-reference nodes leave it empty.
	ReferenceKind ReferenceKind `json:"referenceKind,omitempty"`
	// Binding is present on source import/export items, independently of resolution.
	Binding *ModuleBinding `json:"binding,omitempty"`
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

func cloneNode(n Node) Node {
	n.Tags = append([]Tag(nil), n.Tags...)
	n.Manifest = cloneManifest(n.Manifest)
	if n.Binding != nil {
		b := *n.Binding
		n.Binding = &b
	}
	if n.Location != nil {
		loc := *n.Location
		n.Location = &loc
	}
	if n.NameLocation != nil {
		loc := *n.NameLocation
		n.NameLocation = &loc
	}
	if n.SignatureLocation != nil {
		loc := *n.SignatureLocation
		n.SignatureLocation = &loc
	}
	n.Markers = append([]Marker(nil), n.Markers...)
	n.Documentation = append([]Documentation(nil), n.Documentation...)
	return n
}

func declarationKind(kind string) (NodeKind, error) {
	if concrete := analysis.ConcreteKind(kind); concrete != "" {
		return NodeKind(concrete), nil
	}
	return "", fmt.Errorf("unsupported declaration kind %q", kind)
}

// ReferenceKind is the source syntax role, independent of whether a target exists.
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
	Contains   RelationKind = "contains"
	Declares   RelationKind = "declares"
	Imports    RelationKind = "imports"
	Calls      RelationKind = "calls"
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

// Confidence describes evidence strength, not a calibrated probability.
type Confidence = confidence.Level

const (
	Exact     = confidence.Exact     // established by supported static semantics in the supplied snapshot
	Scoped    = confidence.Scoped    // constrained by bindings, imports, receivers or types
	NameOnly  = confidence.NameOnly  // name match without a proven binding
	Heuristic = confidence.Heuristic // convention or incomplete structural similarity
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

// cloneRelation detaches both the proof list and optional supporting locations.
func cloneRelation(r Relation) Relation {
	r.Evidence = append([]Evidence(nil), r.Evidence...)
	for i := range r.Evidence {
		if r.Evidence[i].Location != nil {
			loc := *r.Evidence[i].Location
			r.Evidence[i].Location = &loc
		}
	}
	return r
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

func extractionDiagnostic(f analysis.Facts, issue analysis.Issue) Diagnostic {
	d := Diagnostic{Code: issue.Code, Message: issue.Message, Subject: DiagnosticSubject(issue.Subject),
		Relation: RelationKind(issue.Relation), Location: location(f, issue.Span)}
	if issue.Outline != nil {
		outline := OutlineCoverage(*issue.Outline)
		d.Outline = &outline
	}
	return d
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), in...)
	for i := range out {
		if out[i].Outline != nil {
			outline := *out[i].Outline
			out[i].Outline = &outline
		}
	}
	return out
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
}

func cloneReport(r BuildReport) BuildReport {
	r.Documents = append([]string(nil), r.Documents...)
	r.Diagnostics = cloneDiagnostics(r.Diagnostics)
	return r
}

// DocumentID identifies a document node by its snapshot-relative logical path.
func DocumentID(name string) string { return "document:" + name }

// declarationID is shared by early detached results and published nodes.
func declarationID(path string, kind NodeKind, qualifiedName string, start int) string {
	return "node:" + identity(path, kind, qualifiedName, start)
}

func identity(parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func location(f analysis.Facts, span analysis.Span) Location {
	line := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.Start })
	endLine := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.End })
	if endLine == 0 {
		endLine = 1
	}
	return Location{Path: f.Path, StartByte: span.Start, EndByte: span.End, Line: line, Column: span.Start - f.LineStarts[line-1] + 1, EndLine: endLine, EndColumn: span.End - f.LineStarts[endLine-1] + 1}
}

func locationPtr(f analysis.Facts, span analysis.Span) *Location {
	loc := location(f, span)
	return &loc
}
