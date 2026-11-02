package codegraph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/pkg"
)

type NodeKind = pkg.NodeKind

const (
	DocumentNodeKind  = pkg.DocumentNodeKind
	DirectoryNodeKind = pkg.DirectoryNodeKind
	Reference         = pkg.Reference
	Import            = pkg.Import
	Export            = pkg.Export
	Struct            = pkg.Struct
	Interface         = pkg.Interface
	Field             = pkg.Field
	Method            = pkg.Method
	Function          = pkg.Function
	Type              = pkg.Type
	TypeAlias         = pkg.TypeAlias
	Class             = pkg.Class
	Constructor       = pkg.Constructor
	Variable          = pkg.Variable
	Constant          = pkg.Constant
	Module            = pkg.Module
	Package           = pkg.Package
	Enum              = pkg.Enum
	Record            = pkg.Record
	Namespace         = pkg.Namespace
	Property          = pkg.Property
	Trait             = pkg.Trait
	Macro             = pkg.Macro
	Union             = pkg.Union
)

type Location = pkg.Location

type Node = pkg.Node

func cloneNode(n Node) Node {
	n.Tags = append([]Tag(nil), n.Tags...)
	n.Manifest = cloneManifest(n.Manifest)
	if n.Resource != nil {
		r := *n.Resource
		n.Resource = &r
	}
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

type ResourceReference = pkg.ResourceReference

type ReferenceKind = pkg.ReferenceKind

const (
	CallReference      = pkg.CallReference
	SymbolReference    = pkg.SymbolReference
	BaseReference      = pkg.BaseReference
	InterfaceReference = pkg.InterfaceReference
	DecoratorReference = pkg.DecoratorReference
)

type ModuleBinding = pkg.ModuleBinding

type RelationKind = pkg.RelationKind

const (
	InDirectory = pkg.InDirectory
	InNamespace = pkg.InNamespace
	Encloses    = pkg.Encloses
	Contains    = pkg.Contains
	Declares    = pkg.Declares
	Imports     = pkg.Imports
	Calls       = pkg.Calls
	References  = pkg.References
	Extends     = pkg.Extends
	Implements  = pkg.Implements
	OccursIn    = pkg.OccursIn
	Aliases     = pkg.Aliases
	Exports     = pkg.Exports
	Decorates   = pkg.Decorates
)

type Confidence = pkg.Confidence

const (
	Exact     = pkg.Exact
	Scoped    = pkg.Scoped
	NameOnly  = pkg.NameOnly
	Heuristic = pkg.Heuristic
)

type Evidence = pkg.Evidence

type Relation = pkg.Relation

type Path = pkg.Path

type Subgraph = pkg.Subgraph

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

type DiagnosticSubject = pkg.DiagnosticSubject

const (
	DocumentSubject     = pkg.DocumentSubject
	DeclarationsSubject = pkg.DeclarationsSubject
	RelationsSubject    = pkg.RelationsSubject
	ContextSubject      = pkg.ContextSubject
	ResourcesSubject    = pkg.ResourcesSubject
)

type Diagnostic = pkg.Diagnostic

type OutlineCoverage = pkg.OutlineCoverage

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

type Documentation = pkg.Documentation

type MarkerKind = pkg.MarkerKind

const (
	Spec = pkg.Spec
	Case = pkg.Case
	Rule = pkg.Rule
	Link = pkg.Link
	Doc  = pkg.Doc
)

type Marker = pkg.Marker

type BuildReport = pkg.BuildReport

type MaterialReport = pkg.MaterialReport

type AnalysisReport = pkg.AnalysisReport

type SelectionReport = pkg.SelectionReport

type PublicationReport = pkg.PublicationReport

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

const (
	Reads = pkg.Reads

	Writes = pkg.Writes

	UsesType = pkg.UsesType

	Captures = pkg.Captures

	UsesResource = pkg.UsesResource

	CallSite = pkg.CallSite

	ReadSite = pkg.ReadSite

	WriteSite = pkg.WriteSite

	TypeUse = pkg.TypeUse

	CaptureSite = pkg.CaptureSite

	ResourceUse = pkg.ResourceUse

	DecoratorUse = pkg.DecoratorUse

	BindsTo = pkg.BindsTo
)
