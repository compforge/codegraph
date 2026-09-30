package graphmodel

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
)

// DocumentID identifies a document node by its snapshot-relative logical path.
func DocumentID(name string) string { return "document:" + name }

// DeclarationID is shared by early detached results and published nodes.
func DeclarationID(path string, kind NodeKind, qualifiedName string, start int) string {
	return "node:" + Identity(path, kind, qualifiedName, start)
}

func Identity(parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func SourceLocation(f analysis.Facts, span analysis.Span) Location {
	line := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.Start })
	endLine := sort.Search(len(f.LineStarts), func(i int) bool { return f.LineStarts[i] > span.End })
	if endLine == 0 {
		endLine = 1
	}
	return Location{Path: f.Path, StartByte: span.Start, EndByte: span.End, Line: line, Column: span.Start - f.LineStarts[line-1] + 1, EndLine: endLine, EndColumn: span.End - f.LineStarts[endLine-1] + 1}
}

func LocationPtr(f analysis.Facts, span analysis.Span) *Location {
	loc := SourceLocation(f, span)
	return &loc
}
