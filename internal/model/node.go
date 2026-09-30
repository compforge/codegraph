package model

import (
	"fmt"

	"github.com/compforge/codegraph/internal/analysis"
)

// NodeKind is the concrete code category used both by Node.Kind and Cypher labels.
// +spec=`Each node has exactly its concrete kind as a graph label; symbol is terminology, not a graph category`
type NodeKind string

const (
	// DocumentKind represents the input material itself, including opaque gitlinks.
	DocumentKind NodeKind = "Document"
	Struct       NodeKind = "Struct"
	Interface    NodeKind = "Interface"
	Field        NodeKind = "Field"
	Method       NodeKind = "Method"
	Function     NodeKind = "Function"
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
	// Gitlink is set only on an opaque gitlink Document, never on symbols.
	Gitlink       string    `json:"gitlink,omitempty"`
	Kind          NodeKind  `json:"kind"`
	Name          string    `json:"name"`
	QualifiedName string    `json:"qualifiedName,omitempty"`
	Language      string    `json:"language"`
	Location      *Location `json:"location,omitempty"`
	Markers       []Marker  `json:"markers,omitempty"`
}

func CloneNode(n Node) Node {
	if n.Location != nil {
		loc := *n.Location
		n.Location = &loc
	}
	n.Markers = append([]Marker(nil), n.Markers...)
	return n
}

func DeclarationKind(kind string) (NodeKind, error) {
	if concrete := analysis.ConcreteKind(kind); concrete != "" {
		return NodeKind(concrete), nil
	}
	return "", fmt.Errorf("unsupported declaration kind %q", kind)
}
