// Package extract converts syntax trees to detached source facts.
package extract

import (
	"context"
	"fmt"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
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
}

// Go uses FactProgram for definitions/calls and go/ast for declaration categories,
// members, lexical binding and documentation ownership. Other grammars are not
// advertised as resolved.
func Analyze(ctx context.Context, name string, source []byte, timeout time.Duration) (Facts, error) {
	f := Facts{Path: name, Language: "go", Source: source}
	f.LineStarts = []int{0}
	for i, b := range source {
		if b == '\n' {
			f.LineStarts = append(f.LineStarts, i+1)
		}
	}
	if err := ctx.Err(); err != nil {
		return f, err
	}
	entry := grammars.DetectLanguageByName("go")
	lang := entry.Language()
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
