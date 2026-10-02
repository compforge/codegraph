# CodeGraph

English | [简体中文](README.zh-CN.md)

An embeddable, multilingual code property graph library written in Go. It statically analyzes input
Documents to build a graph of symbols and their relationships for code review and impact analysis.
The analysis also retains intermediate results, such as file outlines, for consumers to reuse.
It runs in your process, without a separate database service or mandatory disk persistence.

## Capabilities

- Extract declarations, imports, references, calls, type relations, and intent markers from source.
- Analyze Go, Python, JavaScript, TypeScript, and TSX with language-specific binding rules;
  use registered grammars for outline extraction in other languages.
- Connect facts across supplied files, preserving recursion, multiple call sites, and relation evidence.
- Build from memory or caller-provided repository snapshots, including opaque gitlink entries.
- Query nodes, relations, and bounded paths with parameterized, read-only Cypher or Go accessors.
- Report candidate targets and local analysis gaps alongside the usable graph.
- Access file outlines retained during analysis before completing graph construction, including lexical nesting, source ranges, and extraction reports.

CodeGraph provides static evidence within the supplied scope. It does not perform compiler type
checking or guarantee complete runtime dispatch analysis. Repository discovery, Git reads, dependency
acquisition, and decisions about change impact or test selection belong to the caller.

## Core concepts

You provide **Documents**: logical paths paired with source content or a pinned gitlink commit.
An **Extractor** produces reusable single-document **Facts**. A **Builder** binds those facts
with snapshot-specific resolution context and publishes a read-only **Graph**. Typed access needs
no query index; Cypher storage is created when first queried.

Nodes use concrete kinds such as `Document`, `Package`, `Module`, `Class`, and `Function`.
“Symbol” describes declarations; “Namespace” describes how languages organize members.
These roles can overlap: a class both declares a type and organizes members.

Relations connect these nodes: `declares` records source contributions, `contains` records direct
membership, and `imports`, `references`, `calls`, `extends`, and `implements` describe code relationships.
Each relation preserves its occurrence and evidence. Confidence is `exact`, `scoped`, `name_only` or `heuristic`, not a
probability of downstream impact. Missing targets remain diagnostics instead of invented nodes.

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

See the [usage guide](docs/usage.md) (in Chinese) for batch construction and more queries.
