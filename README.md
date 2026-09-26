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

- Extracts **Go** functions, methods, structs, interfaces, fields, other named types, type aliases, and single-name variables and constants, with contains, imports, static package-function calls, and candidate receiver/method-alias calls.
- Extracts **Python, JavaScript, TypeScript, and TSX** declarations, lexical containment, local source imports, explicit module bindings and unshadowed local/imported function calls, candidate class constructors and receiver methods, and declaration-comment markers.
- Accepts other gotreesitter-registered languages through a shared syntax/outline adapter. Missing outlines, unsupported declaration categories, and unavailable reference resolution produce explicit diagnostics.
- Records documents without a registered grammar as file-level nodes without parsing; the coverage gap stays visible as an `unsupported_language` diagnostic.
- Exposes detached per-document facts (declarations, imports with imported names and scope bindings, calls with syntax-based target hints, export aliases, explicit type relations, markers) through `Extract` without publishing graph state; results are cached by content identity so a later `AddDocuments` of the same source never parses twice. A language-neutral `Statements` fact (execution order, scopes, a bounded expression vocabulary) is captured for Python sources today and left empty where capture is not implemented.
- Exposes Go/Python/JS/TS identifier-use facts with locations and declaration owners, and `references` edges with binding evidence.
- Supports cross-file relations, imports within the supplied scope, recursion, multiple call sites, incremental batches, and idempotent additions.
- Accepts source documents directly from memory, Git snapshots, or any other consumer-owned source.
- Accepts parameterized, read-only Cypher and returns Node, Relation, Path, or ordinary Go values.
- Extracts spec, case, rule, link, and doc markers from declaration comments, preserving their contents and source locations.
- Emits candidate relations for ambiguous targets and diagnostics for unresolved targets, unknown callbacks and calls inside unmodeled closures.

This is not a compiler type checker: it does not evaluate build tags, resolve third-party modules,
or guarantee complete dynamic dispatch analysis. Extracts named base-type `extends` and TS/TSX explicit `implements` relations.
Go interface embeddings bind `extends`; same-package direct method-name sets produce candidate
`implements` without signature or pointer-method-set checking. Empty, embedded and type-term interfaces
are excluded from this inference. Struct embedding remains composition, represented by fields and references. A recognized grammar is not a promise of complete language semantics.

Use `Language(path)` for file detection, `Languages()` to list registered grammars, and
`Capabilities()` for the Go/Python/JS/TS/TSX adapters. `Capabilities("rust", "java")` inspects additional
outline capabilities on demand; it does not eagerly load every parser. Unknown names return no capability.

Python imports use repository-relative module candidates; absolute imports remain `candidate` because
runtime search paths are unknown. JS/TS imports resolve relative source paths, including index files;
multiple matching files remain candidates. Package metadata, tsconfig aliases, Python package initialization,
and runtime dispatch are not evaluated. Named/default/namespace imports and explicit re-export chains
resolve against supplied sources; symbol imports, references, and calls retain binding confidence. Returned paths
describe evidence within these declared limits, not compiler or runtime equivalence.

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
distinguishes `declaration` from `receiver_declaration`; ambiguous receivers produce candidate edges,
and unresolved receivers produce diagnostics. Anonymous nested types and promoted members are not expanded.

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
// Local gaps accompany the usable graph. Consumers decide their relevance.
for _, diagnostic := range report.Diagnostics {
    fmt.Printf("%s: %s (%s)\n", diagnostic.Location.Path, diagnostic.Code, diagnostic.Subject)
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

You can also create an empty graph with `New(snapshot, options)`. `AddDocuments` queues a batch;
`AddDocument` queues one document and returns a `ResultTask[Facts]`. `GetDocument(document.ID())`
and `FindAsync(path, kind, qualifiedName)` expose document and declaration results before `Wait`.
Background work resolves cross-document relations and atomically publishes the queryable graph
without a `Wait` call. `Wait` waits for work submitted before the call and returns its report;
later submissions may share that publication. Queries read the previous published batch while a build is in progress.

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
main := codegraph.Document{Path: "main.go", Content: []byte("package demo\nfunc Entry(){ Work() }")}
if err := g.AddDocuments(ctx, main,
    codegraph.Document{Path: "work.go", Content: []byte("package demo\nfunc Work(){}")},
); err != nil {
    return err
}
task, err := g.GetDocument(main.ID())
if err != nil { return err } // The document must have been submitted.
if _, err := task.Wait(); err != nil { return err } // Facts are ready before Wait.
report, err := g.Wait(ctx)
if err != nil {
    return err
}
for _, diagnostic := range report.Diagnostics {
    fmt.Printf("%s: %s (%s)\n", diagnostic.Location.Path, diagnostic.Code, diagnostic.Subject)
}
```

- Paths use slash-separated, snapshot-relative names valid under `fs.ValidPath`, not absolute paths or URLs.
- `Document.ID()` is the corresponding File node ID (`FileID(Path)`) within the Graph snapshot.
- `AddDocuments` queues documents without returning one task per document. Use `GetDocument(ID)` for early
  facts or `FindAsync` for detached declarations. `GetDocument` returns `ErrDocumentNotFound` for an ID that
  has not been submitted. `AddDocument` returns its task directly.
- Background builds resolve references against submitted and previously loaded sources, apply budgets,
  and atomically publish the graph. No dependencies are fetched implicitly. `Wait` only waits for the
  submitted work and returns the build report; canceling that wait does not cancel the build.
- Repeated identical input is idempotent. Conflicting content returns `ErrSnapshotChanged`. Content is copied
  when admitted, so callers may reuse their input buffer after `AddDocument` or `AddDocuments` returns.

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

Build and Wait return execution failures through `error`. A successful publication may contain
candidate edges and local information gaps. `BuildReport.Diagnostics` identifies each gap's
`subject` (document, declarations, relations, context, or resources), source range, and affected
relation kind when known. Candidate edges retain `confidence` and `basis` without duplicate
ambiguity diagnostics. Missing targets remain diagnostics; no target node is invented.

Outline omissions expose structured `outline` counters. These count query candidates rather than
all declarations in the source. The upstream outliner does not provide omitted ranges, so an
outline counter diagnostic covers the whole document. Duplicate candidates alone do not lose
information and do not produce a gap. Existing imports and unrelated declarations remain usable.

`Query` converts results to entities, paths, strings, int64, float64, bool, null, lists, and maps.
Other Cypher-specific value types return an error.

- Queries cannot write or invoke procedures; variable-length paths require an explicit upper bound.
- `Options.BuildConcurrency` bounds parallel document extraction per graph: `0` uses `min(GOMAXPROCS, 4)`, `1` is serial, and positive values set the worker limit. Negative values are rejected. Separate batches remain serialized; nodes and relations are assembled and published atomically after extraction. When building multiple graphs concurrently, callers should budget their combined worker count.
- Options provide finite default budgets for file count, source size, node/edge count, parsing/query timeouts, path depth, and result size.
- Query errors return no partial rows. Parse failures preserve File identities with document diagnostics; usable facts from other documents are published. Budget failures, snapshot conflicts, and cancellation roll back the entire batch.

## Local validation

```sh
make fmt
make lint test build
```

Tests cover multilingual outlines and bindings, grammar extensions, cross-file imports/calls, parallel edges, marker round trips, path confidence
filters, read-only queries and budgets, batch rollback, and concurrent queries.
See the [kernel design](docs/kernel.md) (in Chinese) for source structure and design rationale.
