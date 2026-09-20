// Package extract converts syntax trees to detached source facts.
package extract

import (
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
type Import struct {
	Alias, Path string
	From        string
	Relative    int
	Span
}
type Call struct {
	Name, Receiver string
	Span
	// Blocked means syntax proves this is not a resolvable static function reference.
	Blocked bool
	Builtin bool
}
type Facts struct {
	Path, Package, Language string
	Source                  []byte
	LineStarts              []int
	Declarations            []Declaration
	Imports                 []Import
	Calls                   []Call
	Issues                  []Issue
}

type Issue struct {
	Code, Message string
	Span
}

// Analyze releases the syntax tree before returning detached facts. Language
// detection is registry-driven; language-specific binding rules never leak into
// the graph model or the batch publication path.
func Analyze(ctx context.Context, name string, source []byte, timeout time.Duration) (Facts, error) {
	f := Facts{Path: name, Source: source}
	f.LineStarts = []int{0}
	for i, b := range source {
		if b == '\n' {
			f.LineStarts = append(f.LineStarts, i+1)
		}
	}
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
		f.Imports = append(f.Imports, Import{Alias: i.Alias, Path: i.Path, Span: Span{int(i.StartByte), int(i.EndByte)}})
	}
	if err := enrichGo(&f); err != nil {
		return f, err
	}
	return f, ctx.Err()
}
