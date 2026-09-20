package codegraph

type NodeKind string

const (
	File   NodeKind = "File"
	Symbol NodeKind = "Symbol"
)

// Location uses zero-based byte offsets (end exclusive) and one-based lines/columns.
// Columns count bytes, not Unicode code points.
type Location struct {
	Path      string `json:"path"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
}

// Node is a file or declaration in a single source snapshot.
type Node struct {
	ID            string   `json:"id"`
	Kind          NodeKind `json:"kind"`
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualifiedName,omitempty"`
	SymbolKind    string   `json:"symbolKind,omitempty"`
	Language      string   `json:"language"`
	Location      Location `json:"location"`
	Markers       []Marker `json:"markers,omitempty"`
}

func cloneNode(n Node) Node {
	n.Markers = append([]Marker(nil), n.Markers...)
	return n
}
