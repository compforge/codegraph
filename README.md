# CodeGraph

English | [简体中文](README.zh-CN.md)

CodeGraph is a multilingual code analysis library you can embed in a Go program. Supply a set of source
files, and it statically builds a graph of symbols and their relationships. Code review, impact analysis,
and source navigation tools can query those connections and inspect the evidence behind them.
It runs in your process, without a separate database service or a requirement to write source files to disk.

## What it helps you answer

Code tools often start from a change or a symbol and need to find code worth examining next:

- **Calls and references:** Who calls this function? Which declarations reference this type?
- **Structure and dependencies:** What members belong to a type? Which modules does a file import?
  Which types declare inheritance or implementation relationships?
- **Evidence:** Where does a connection occur in the source? Is its target established or only a candidate?
  Which locations remain unresolved?

CodeGraph parses and organizes code facts, exposing queryable symbols, relationships, and evidence.
Applications apply their own rules to those facts: repocli determines change impact and test scope,
while CCR determines review scope and assembles review context.

## Inputs and results

```text
Source files (path + content) → Static analysis → Symbol graph → Relationship and evidence queries
```

**A Document is an input material.** It usually contains a logical path and complete source content,
obtained from memory, a working tree, or a Git revision. The path need not exist on disk, but all materials
in one graph must belong to the same snapshot. You select and read the materials; CodeGraph analyzes
the supplied scope without scanning repositories or downloading dependencies.

**A Graph is the analysis result.** Declarations such as functions, types, and fields form symbol nodes.
Calls, references, membership, imports, and type relationships connect them. The graph also retains
files and organizational entities such as packages and modules. For example, when `Entry()` calls
`Work()`, the graph records `Entry → calls → Work`: you can find callees from Entry or callers from Work.
Go accessors and read-only Cypher queries return nodes, relationships, and their source locations.
Consumer views derive their code facts exclusively from these nodes and relationships.

**Evidence and diagnostics tell you what conclusions a result supports.** Each relationship retains its
derivation and confidence. Some targets are established within the supplied materials; others are
candidates constrained by names, imports, or type information. Missing targets and unsupported analysis
leave local diagnostics. A candidate may warrant further inspection, and an absent relationship does
not prove that no connection exists elsewhere in the repository.

Go, Python, JavaScript, TypeScript, and TSX have language-specific name binding and relationship analysis.
Other registered grammars primarily provide declaration structure. Coverage varies by language and code
construct; see [language support and limitations](docs/language-support.md) (in Chinese).
CodeGraph does not perform compiler type checking or guarantee complete runtime dispatch analysis.

## Quick start

Use the Go toolchain required by [go.mod](go.mod), then add the library to your application:

```sh
go get github.com/compforge/codegraph
```

This program builds a graph and finds callers of `Work`:

```go
package main

import (
    "context"
    "fmt"

    "github.com/compforge/codegraph"
)

func main() {
    ctx := context.Background()
    g, report, err := codegraph.Build(ctx, "revision-1", []codegraph.Document{
        {Path: "main.go", Content: []byte("package demo\nfunc Entry(){Work()}\nfunc Work(){}")},
    }, codegraph.Options{})
    if err != nil {
        panic(err)
    }
    for _, diagnostic := range report.Diagnostics {
        fmt.Printf("%s: %s\n", diagnostic.Location.Path, diagnostic.Code)
    }
    rows, err := g.Query(ctx, `
        MATCH (caller:Function)-[:calls]->(:Function {name:$name})
        RETURN caller.name AS caller`, map[string]any{"name": "Work"})
    if err != nil {
        panic(err)
    }
    for _, row := range rows {
        fmt.Println(row["caller"])
    }
}
```

Output: `Entry`. Successful construction may still report local analysis gaps; callers decide
which gaps matter to their task. More executable examples are in [example_test.go](example_test.go).

## Further reading

The following guides are in Chinese:

- [Usage guide](docs/usage.md): add materials in batches, reuse analysis results, locate source, query relationships, and derive file outlines from the graph.
- [Document contract](docs/document.md): material identity, snapshot consistency, and Git submodule boundaries.
- [Kernel design](docs/kernel.md): analysis stages and responsibilities for contributors and extension authors.
