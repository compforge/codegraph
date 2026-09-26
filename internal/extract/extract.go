// Package extract converts syntax trees to detached source facts.
package extract

import (
	"bytes"
	"context"
	"fmt"
	"time"

	gts "github.com/odvcencio/gotreesitter"
)

type Span struct{ Start, End int }
type Declaration struct {
	Name, QualifiedName, Kind string
	// Parent is the enclosing declaration index, or -1 for a file-level declaration.
	Parent int
	// Receiver is the explicit Go receiver base name, resolved across loaded files.
	Receiver string
	Span
	Comments []Comment
}
type Comment struct {
	Kind, Text string
	Span
}

// ImportBinding distinguishes imported names from the local names they bind.
type ImportBinding struct {
	Name, Local         string
	Namespace, ReExport bool
	Span                // complete statement, for lexical scope and shadow checks
}

type Import struct {
	Alias, Path string
	From        string
	Relative    int
	// Binding is the name the import introduces in this lexical scope.
	Binding string
	// Names are the imported names before caller aliases; empty means the whole
	// module is imported. Bindings retain per-name aliases for resolution.
	Names    []string
	Bindings []ImportBinding
	Span
}

// CallTarget is a detached syntax hypothesis, not a runtime dispatch result.
type CallTarget struct {
	Name, ReceiverType, Module string
	Kind, Basis                string
}

type Call struct {
	Name, Receiver string
	Span
	// Blocked excludes the static-function path; Targets may still carry dispatch candidates.
	Blocked  bool
	Builtin  bool
	Imported bool
	Targets  []CallTarget
}

// Reference is a lexical identifier use, independently of whether a target exists.
type Reference struct {
	Name, Receiver string
	Span
	Owner  int  // enclosing declaration, -1 for the file
	Target int  // same-file declaration when syntax proves binding, -1 otherwise
	Bound  bool // a lexical binding exists; do not substitute a same-named declaration
}

// TypeRelation records an explicit base/interface name before scope binding.
type TypeRelation struct {
	Owner                     int
	Name, Module, Kind, Basis string
	Span
}

type Facts struct {
	Path, Package, Language string
	Source                  []byte
	LineStarts              []int
	Declarations            []Declaration
	Imports                 []Import
	Calls                   []Call
	References              []Reference
	TypeRelations           []TypeRelation
	Issues                  []Issue
	// Exports maps a public alias to its local name for explicit export
	// aliases; extraction records them for consumer-side module resolution.
	Exports map[string]string
	// Python preserves statement order and scope for Python sources; its
	// bounded expression vocabulary is interpreted by consumers.
	Python []PythonStatement
}

type Issue struct {
	Code, Message     string
	Subject, Relation string
	Span
	Outline *OutlineCoverage
}

// OutlineCoverage is a detached receipt for the declaration query. Keeping
// this value independent of the parser makes cached facts safe to publish.
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

func lineStarts(source []byte) []int {
	starts := []int{0}
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// FileOnly records a document that has no registered grammar. The file still
// enters the graph as a file-level fact — without parsing, so no declarations,
// imports, or calls — mirroring how reference code-graph indexers track
// file-level-only languages (stored file record, zero symbol nodes).
func FileOnly(name string, source []byte) Facts {
	return Facts{Path: name, Source: source, LineStarts: lineStarts(source)}
}

// Analyze releases the syntax tree before returning detached facts. Language
// detection is registry-driven; language-specific binding rules never leak into
// the graph model or the batch publication path.
func Analyze(ctx context.Context, name string, source []byte, timeout time.Duration) (Facts, error) {
	f := Facts{Path: name, Source: source}
	f.LineStarts = lineStarts(source)
	if err := ctx.Err(); err != nil {
		return f, err
	}
	entry := Detect(name)
	if entry == nil {
		return f, fmt.Errorf("no grammar for %s", name)
	}
	f.Language = entry.Name
	if entry.Language == nil {
		return f, fmt.Errorf("grammar %s has no loader", entry.Name)
	}
	lang := entry.Language()
	if lang == nil {
		return f, fmt.Errorf("grammar %s is unavailable", entry.Name)
	}
	p := gts.NewParser(lang)
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	p.SetTimeoutMicros(uint64(max(timeout.Microseconds(), 1)))
	var tree *gts.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		tree, err = p.ParseWithTokenSourceStrict(source, entry.TokenSourceFactory(source, lang))
	} else {
		tree, err = p.ParseStrict(source)
	}
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		return f, fmt.Errorf("parse %s: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return f, err
	}
	if tree == nil || tree.RootNode() == nil || tree.RootNode().HasErrorOrMissing() {
		return f, fmt.Errorf("parse %s: incomplete syntax tree", name)
	}
	if f.Language != "go" {
		return analyzeOutline(ctx, f, tree, *entry)
	}
	program, err := gts.NewFactProgram(lang, gts.FactDefinitions|gts.FactCalls|gts.FactImports)
	if err != nil {
		return f, err
	}
	facts := program.Extract(tree)
	for _, d := range facts.Definitions {
		f.Declarations = append(f.Declarations, Declaration{Name: d.Name, QualifiedName: d.Name, Kind: d.Kind, Span: Span{int(d.StartByte), int(d.EndByte)}})
	}
	for _, c := range facts.Calls {
		f.Calls = append(f.Calls, Call{Name: c.Name, Receiver: c.Receiver, Span: Span{int(c.StartByte), int(c.EndByte)}})
	}
	for _, i := range facts.Imports {
		if i.Kind == "package" {
			f.Package = i.Name
			continue
		}
		f.Imports = append(f.Imports, Import{Alias: i.Alias, Path: i.Path, Binding: i.Name, Span: Span{int(i.StartByte), int(i.EndByte)}})
	}
	if bytes.Contains(source, []byte("//go:embed")) {
		f.Issues = append(f.Issues, Issue{Code: "unsupported_resource", Message: "go:embed dependencies are not resolved", Subject: "resources", Span: Span{End: len(source)}})
	}
	if err := enrichGo(&f); err != nil {
		return f, err
	}
	return f, ctx.Err()
}
