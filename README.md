# CodeGraph

English | [简体中文](README.zh-CN.md)

An embeddable code property graph library for Go. Code review and impact analysis tools can use it
to query related files, symbols, and the evidence behind their relationships.

CodeGraph parses source code with gotreesitter and uses GoGraph for an in-memory property graph
and Cypher queries. Nodes use concrete kinds such as File, Struct, Interface, Field, Method, and
Function; relations include calls, imports, and containment. Declaration nodes carry structured
intent markers: spec, case, rule, link, and doc.

Direct dependencies: gotreesitter `v0.52.0` and GoGraph `v0.15.0`. Requires Go 1.26 or later.
No separate database service or mandatory disk persistence. The project has not had its first release yet.

## Capabilities and limits

- Extracts **Go** functions, methods, structs, interfaces, fields, other named types, and type aliases, with contains, imports, and static package-function calls.
- Supports cross-file relations, imports within the module, recursion, multiple call sites, on-demand expansion, and idempotent additions.
- Accepts parameterized, read-only Cypher and returns Node, Relation, Path, or ordinary Go values.
- Extracts spec, case, rule, link, and doc markers from declaration comments, preserving their contents and source locations.
- Emits candidate relations for ambiguous targets and diagnostics for unresolved targets, callbacks, calls inside closures, and receiver calls.

This is not a compiler type checker: it does not evaluate build tags, resolve third-party modules,
or guarantee complete dynamic dispatch analysis. Other languages and automatic extraction of
references, extends, and implements are not yet supported. Inspect `Capabilities()` for supported features.

## Node kinds

`Node.Kind` is also the node's Cypher label: `File`, `Struct`, `Interface`, `Field`, `Method`,
`Function`, `Type`, or `TypeAlias`. `Type` covers other named types such as `type ID int`;
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
    os.DirFS("./repo"),
    []string{"main.go"},
    codegraph.Options{
        ModulePath:    "example.org/demo",
        ExpandImports: true,
    },
)
if err != nil {
    return err
}
// report.Complete covers only the requested/expanded scope, not the whole repository.
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

You can also create an empty graph with `New(snapshot, options)` and add files using
`AddFiles(ctx, source, paths...)`. Each addition rebuilds the current local graph before publishing
it atomically. Queries can continue reading the previous batch while the replacement is being built.
This is a correctness-first batch update, not an incremental graph-engine optimization.

Use separate Graph instances for before/after snapshots. Adding the same path with different bytes
returns `ErrSnapshotChanged`. The caller is responsible for `fs.FS` immutability and file-access boundaries.

## Markers and query properties

```go
// +spec=`Calls must preserve source locations`
// +case:id=parallel,expect=`Preserve both call sites`
// +rule=`Do not merge relations by endpoints`
// +link=docs/kernel.md
// +doc=`Entry function`
func Entry() { Work(); Work() }
```

Markers are attached through declaration doc comments. Structured payloads are preserved verbatim;
expressions are not evaluated.

| Object | Common properties |
|---|---|
| Node | id, kind, name, qualifiedName, language, path, line, column, startByte, endByte, snapshot |
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

Tests cover cross-file calls, aliased imports, parallel edges, marker round trips, path confidence
filters, read-only queries and budgets, batch rollback, and concurrent queries.
See the [kernel design](docs/kernel.md) (in Chinese) for source structure and design rationale.
