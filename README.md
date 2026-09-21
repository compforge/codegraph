# CodeGraph

English | [简体中文](README.zh-CN.md)

An embeddable, multilingual code property graph library written in Go. Code review and impact analysis tools can use it
to query related files, symbols, and the evidence behind their relationships.

CodeGraph parses source code with gotreesitter and uses GoGraph for an in-memory property graph
and Cypher queries. Nodes use concrete kinds such as File, Struct, Interface, Field, Method, and
Function; relations include calls, imports, and containment. Declaration nodes carry structured
intent markers: spec, case, rule, link, and doc.

Direct dependencies: gotreesitter `v0.52.0` and GoGraph `v0.15.0`. Requires Go 1.26 or later.
No separate database service or mandatory disk persistence. The project has not had its first release yet.

## Capabilities and limits

- Extracts **Go** functions, methods, structs, interfaces, fields, other named types, type aliases, and single-name variables and constants, with contains, imports, and static package-function calls.
- Extracts **Python, JavaScript, TypeScript, and TSX** declarations, lexical containment, local source imports, unshadowed same-file module-function calls, and declaration-comment markers.
- Accepts other gotreesitter-registered languages through a shared syntax/outline adapter. Missing outlines, unsupported declaration categories, and unavailable reference resolution produce explicit diagnostics.
- Supports cross-file relations, imports within the supplied scope, recursion, multiple call sites, incremental batches, and idempotent additions.
- Accepts source documents directly from memory, Git snapshots, or any other consumer-owned source.
- Accepts parameterized, read-only Cypher and returns Node, Relation, Path, or ordinary Go values.
- Extracts spec, case, rule, link, and doc markers from declaration comments, preserving their contents and source locations.
- Emits candidate relations for ambiguous targets and diagnostics for unresolved targets, callbacks, calls inside closures, and receiver calls.

This is not a compiler type checker: it does not evaluate build tags, resolve third-party modules,
or guarantee complete dynamic dispatch analysis. Automatic extraction of references, extends, and
implements is not yet supported. A recognized grammar is not a promise of complete language semantics.

Use `Language(path)` for file detection, `Languages()` to list registered grammars, and
`Capabilities()` for the Go/Python/JS/TS/TSX adapters. `Capabilities("rust", "java")` inspects additional
outline capabilities on demand; it does not eagerly load every parser. Unknown names return no capability.

Python imports use repository-relative module candidates; absolute imports remain `candidate` because
runtime search paths are unknown. JS/TS imports resolve relative source paths, including index files;
multiple matching files remain candidates. Package metadata, tsconfig aliases, Python package initialization,
re-exports as symbol bindings, imported calls, and runtime dispatch are not evaluated. A `Complete` report
covers extracted facts within these declared limits, not compiler or runtime equivalence.

## Node kinds

`Node.Kind` is also the node's Cypher label: `File`, `Struct`, `Interface`, `Field`, `Method`,
`Function`, `Type`, `TypeAlias`, `Class`, `Variable`, `Enum`, and other concrete declaration categories.
Available categories vary by language; consult `Capabilities(language)`. `Type` covers other named types such as `type ID int`;
`TypeAlias` represents explicit aliases such as `type Alias = ID`. Categories describe declarations,
not inferred underlying types. “Symbol” is a term for code declarations, not a graph kind or label.

Fields and explicitly declared interface methods are independent nodes with their own locations
and markers. Query members directly:

```cypher
MATCH (s:Struct)-[:contains]->(f:Field)
RETURN s, f
```

`contains` records lexical ownership. Receiver methods also have a `contains` edge from their
receiver type when it is found in the loaded package, including across files. The relation's `basis`
distinguishes `declaration` from `receiver_declaration`; unresolved or ambiguous receivers produce
diagnostics. Anonymous nested types and promoted members are not expanded.

## Quick start

The following snippet is intended for a consumer application. See [example_test.go](example_test.go)
for a complete, runnable example.

```go
g, report, err := codegraph.Build(
    ctx,
    "revision-1", // Caller-provided identity for an immutable source snapshot.
    []codegraph.Document{
        {Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")},
        {Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
    },
    codegraph.Options{
        ModulePath: "example.org/demo",
    },
)
if err != nil {
    return err
}
// report.Complete covers only the supplied scope, not the whole repository.
// Inspect report.Diagnostics before deciding whether to fall back to another analysis method.
if !report.Complete {
    return fmt.Errorf("partial code graph: %v", report.Diagnostics)
}

rows, err := g.Query(ctx, `
    MATCH p=(caller:Function)-[:calls*1..3]->(target:Function {name:$name})
    WHERE all(r IN relationships(p) WHERE r.confidence = 'exact')
    RETURN caller, p`, map[string]any{"name": "Work"})
if err != nil {
    return err
}
for _, row := range rows {
    caller := row["caller"].(codegraph.Node)
    path := row["p"].(codegraph.Path)
    // caller and path are detached from the underlying engine and ready for consumer use.
    _, _ = caller, path
}
```

You can also create an empty graph with `New(snapshot, options)` and add documents using
`AddDocuments(ctx, documents...)`. Each addition rebuilds the current local graph before publishing
it atomically. Queries can continue reading the previous batch while the replacement is being built.
This is a correctness-first batch update, not an incremental graph-engine optimization.

Use separate Graph instances for before/after snapshots. Adding the same path with different bytes
returns `ErrSnapshotChanged`. The caller owns document selection and source access boundaries.

The same `Build`/`AddDocuments` entrypoints accept mixed-language documents, such as
`[]codegraph.Document{{Path: "server.go"}, {Path: "worker.py"}, {Path: "web/app.ts"}}`. Names in different languages do not bind to one another.
`ModulePath` controls Go module imports only; all languages share scope, budget, and atomic-publication rules.

### Source documents

A `Document` is one source input: a logical `Path` and its complete `Content`. The path need not
exist on disk; it identifies the source within the graph snapshot and determines language detection,
relative-import context, and source locations. Documents are inputs, not a `Node.Kind`.

Use documents when source bytes already come from memory or a Git revision:

```go
g, err := codegraph.New("revision-1", codegraph.Options{})
if err != nil {
    return err
}
report, err := g.AddDocuments(ctx,
    codegraph.Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")},
    codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
)
if err != nil {
    return err
}
if !report.Complete {
    return fmt.Errorf("partial code graph: %v", report.Diagnostics)
}
```

- Paths use slash-separated, snapshot-relative names valid under `fs.ValidPath`, not absolute paths or URLs.
- `AddDocuments` resolves references against the supplied batch and previously loaded sources. It does not
  fetch dependencies; provide every source unit needed for the intended graph explicitly.
- Document batches share scope, budgets, diagnostics, and snapshot identity. Repeated identical input is
  idempotent; conflicting content at a loaded path returns `ErrSnapshotChanged` and rolls back the batch.
- Keep content unchanged during the call. After it returns, the graph owns its retained bytes.

### Locating declarations

Consumers can locate declarations without rebuilding an ID scheme:

```go
entries := graph.Find("src/service.py", codegraph.Function, "Service.run")
for _, entry := range entries {
    callers := graph.RelationsTo(entry.ID, codegraph.Calls)
    _ = callers
}
```

`Find` is ordered by source position; `Node`, `RelationsFrom`, and `RelationsTo` return detached values.
Node locations include both start and end line/column, so diff consumers do not need to parse source again
just to map a changed range to a declaration. An empty kind or qualified name is a wildcard.

## Language extension

Language recognition is not a fixed CodeGraph allowlist. Register additional grammars through
gotreesitter's `grammars.Register` / `RegisterExtension` before building graphs. The shared adapter uses
the grammar's tags and ownership rules to create concrete declaration nodes and `contains` relations.
Syntax trees stay internal. Unknown declaration categories are reported instead of becoming a generic
`Symbol` node. Reference resolution requires language-specific binding rules; outline-only languages always
report partial coverage. The executable [extension test](language_extension_test.go) demonstrates this boundary.

## Markers and query properties

```go
// +spec=`Calls must preserve source locations`
// +case:id=parallel,expect=`Preserve both call sites`
// +rule=`Do not merge relations by endpoints`
// +link=docs/kernel.md
// +doc=`Entry function`
func Entry() { Work(); Work() }
```

Markers are attached through declaration doc comments (Go), or preceding `#` / `//` / `/* */` comments
(Python and JS/TS, including export wrappers). Structured payloads are preserved verbatim;
expressions are not evaluated.

| Object | Common properties |
|---|---|
| Node | id, kind, name, qualifiedName, language, path, line, column, endLine, endColumn, startByte, endByte, snapshot |
| Declaration marker | markers (list of kinds), spec/case/rule/link/doc (lists of contents), markerData (full structure as JSON) |
| Relation | id, kind, source, target, confidence, basis, path, line, column, startByte, endByte |

Confidence is `exact` or `candidate`, not a probability. `exact` means a unique syntactic binding
within the loaded scope, subject to the declared language capabilities. Use `n.id` / `r.id` for
identity; Cypher's `id(n)` is an internal engine identifier, not a source identity.
Source byte ranges are half-open; line numbers and byte columns are 1-based.

`Query` converts results to entities, paths, strings, int64, float64, bool, null, lists, and maps.
Other Cypher-specific value types return an error.

- Queries cannot write or invoke procedures; variable-length paths require an explicit upper bound.
- Options provide finite default budgets for file count, source size, node/edge count, parsing/query timeouts, path depth, and result size.
- Query errors return no partial rows. File-read or parse failures publish a partial graph with diagnostics; budget failures, snapshot conflicts, and cancellation roll back the entire batch.

## Local validation

```sh
make fmt
make lint test build
```

Tests cover multilingual outlines and bindings, grammar extensions, cross-file imports/calls, parallel edges, marker round trips, path confidence
filters, read-only queries and budgets, batch rollback, and concurrent queries.
See the [kernel design](docs/kernel.md) (in Chinese) for source structure and design rationale.
