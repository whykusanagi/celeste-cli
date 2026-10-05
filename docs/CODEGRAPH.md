# Code Graph: Algorithms & Architecture

Technical reference for Celeste's code graph subsystem (`cmd/celeste/codegraph/`).

## Overview

The code graph provides structural understanding of codebases through three search layers:

| Layer | Example Query | What It Finds | When To Use |
|-------|--------------|---------------|-------------|
| **Graph** | "what calls X", "callers of Y" | Call relationships | You know a specific symbol |
| **Semantic (MinHash)** | "code related to authentication" | Conceptually related symbols | You have a concept, not a name |
| **Keyword** | "validateSession" | Exact name matches | You know the exact name |

## Storage

SQLite (WAL mode) via `modernc.org/sqlite` (pure Go, no CGo). Three tables:

```sql
symbols (id, name, kind, package, file, line, signature, decorators, base_classes,
         qual_name, implements, minhash BLOB)
edges   (source_id, target_id, kind)  -- directional, unique on (src, dst, kind)
files   (path, language, size, content_hash, indexed_at, resolution)
meta    (key, value)                  -- minhash_seeds, graph_version, go_modules
```

Indexed on `symbols.name`, `symbols.file`, `symbols.package`, `symbols.qual_name`, `edges.source_id`, `edges.target_id`.

`qual_name` is the type-checked qualified name of a Go symbol, as go/types
prints it: `pkg/path.Func`, `(*pkg/path.T).Method`, `(pkg/path.Iface).Method`.
`implements` lists the interfaces a Go method satisfies (`error,fmt.Stringer`).
`files.resolution` is `typed` or `approximate` for Go files (see below).

`meta.graph_version` records the edge format. When it differs from the
version the binary expects (or is missing), `Update` rebuilds the index from
scratch instead of mixing old and new edges. Version 2 is the type-checked Go
graph (#375).

Database stored at `~/.celeste/projects/<sha256-prefix>/codegraph.db` to avoid polluting project directories.

## Parsing

Two strategies depending on language:

### Go (type-checked)

Go files are indexed together, package by package, with `go/parser` and
`go/types` (standard library only, no CGo). Symbols (functions, methods,
interface methods, types, interfaces, structs, consts, vars, imports) come
from the AST; call edges come from the type checker, so each edge points at
the exact function or method:

- **Packages.** Files are grouped by directory and package clause; a
  package's in-package `_test.go` files are checked with it, the external
  `_test` package separately (in-package tests are checked in a
  test-augmented copy of the package, so test-only imports never create
  import cycles). Files excluded by build constraints for the current
  GOOS/GOARCH are not part of any package. The pass analyses the
  `CGO_ENABLED=0` build (what release binaries are), so cgo-tagged files and
  files importing `"C"` take the fallback on every host. The import path comes from
  the nearest `go.mod` (a directory without one gets `_/<dir>`).
- **Imports.** Workspace packages import each other from source with bodies.
  Everything else (standard library, module dependencies) is type-checked
  from source without function bodies. Their directories come from one
  `go list -e -deps -test ./...` per module, run with `GOPROXY=off` and
  `GOTOOLCHAIN=local` so indexing never downloads anything. Without the `go`
  command only the standard library resolves (through GOROOT).
- **Direct calls.** `f()`, `pkg.F()`, `x.M()` and `a.b.c.M()` resolve
  through `types.Info`, so a call to `update` in package `b` reaches
  `b.update`, never a same-named function elsewhere, and `t.Update()` reaches
  the `Update` of `t`'s type. Generic calls resolve to the generic
  declaration.
- **Function values.** Every place a function value can be stored
  (variable, struct field, parameter, result, map/slice/array element) is a
  slot. Assignments, composite literals, call arguments, returns, `range` and
  channel sends record which functions (and which other slots) flow into which
  slots; the flows are propagated module-wide, flow- and
  instance-insensitively. A call through a slot (`f()`, `s.handler()`,
  `handlers[k]()`, `factory()()`) gets a `calls` edge to every function that
  can reach it. Taking a function as a value (`f := x.M`, `register(h)`,
  `T{Run: run}`) also records a `references` edge from the enclosing symbol.
- **Interfaces.** Each interface method is a symbol (kind
  `interface_method`). A call through an interface calls the interface
  method, which has an `implements` edge to every module method that
  implements it (value or pointer receiver, promoted methods included).
  Methods that satisfy an interface from outside the module (`error`,
  `fmt.Stringer`, `sort.Interface`, a framework's handler interface) have no
  module caller; their `implements` column records the interface, and
  code review and search do not call them dead.

**Fallback.** A file that is not part of a type-checked package (build
constraints, package clause mismatch) is indexed with the old AST heuristic
(bare-name calls). A package with type errors (broken code, a dependency
missing from the module cache) still gets typed edges wherever the checker
resolved the callee, and heuristic edges for the rest. Both cases are
recorded as `files.resolution = approximate`; `code_graph` notes it on the
symbol, search adds the warning `approximate call graph: Go file did not
type-check`, code review appends it to stub reasons, and `celeste index`
prints how many Go files were type-checked.

Known limits: function values are tracked per slot, not per instance (every
`S` value shares the targets stored in `S.h`); calls through closures and
through values that leave the module and come back are not followed; generic
types do not get interface `implements` edges.

### Other Languages (regex-based, broad coverage)

Covers Python, JavaScript, TypeScript, and Rust. Language-specific regex patterns extract declarations line-by-line (functions, classes, interfaces, imports, types, consts). Call edges use a `\b(\w+)\s*\(` heuristic -- matches any `identifier(` pattern, then filters to only known symbol names in the file. Keywords are excluded via a language-aware stop list.

Python class/method detection uses indentation tracking to distinguish top-level functions from methods inside classes.

### Tradeoff

Go gets type-checked call graphs. Other languages get fast-but-approximate
extraction with heuristic call detection.

## Indexing

### Full Build

Walks the file tree respecting `.gitignore` + a hardcoded skip list (`node_modules`, `vendor`, `venv`, `.git`, `dist`, `build`, `target`, etc.). For each indexable file: parse, store symbols, resolve edges, compute MinHash signatures, record file metadata. A full build starts from an empty graph (symbols, edges, files and BM25/LSH rows are cleared; MinHash seeds are kept).

### Incremental Updates

SHA-256 content hash per file. On update:
1. Deleted files: remove their symbols and edges
2. Changed files: delete old symbols, re-index
3. Unchanged files: skip

Go is the exception: if any Go file was added, changed or removed, the Go
pass re-runs over all Go files (it needs whole packages), re-stores the
symbols of changed files (and of any file whose symbols changed meaning),
and rewrites every Go-sourced edge in one transaction. Edges into a changed
file from unchanged callers are therefore kept. A change to a module's
`go.mod` or `go.sum` (tracked as a fingerprint in `meta.go_modules`) re-runs
the Go pass too, since it can change every qualified name.

### Edge Resolution

Go edges from type-checked files carry qualified names and resolve exactly
(or not at all, when the target is outside the workspace). Heuristic edges
(other languages, Go files that did not type-check) store symbol names and
resolve at insert time:
1. Check local file symbols first
2. Fall back to global DB lookup by name
3. Strip qualifier prefix (`pkg.Func` -> `Func`) as a last resort

## Similarity Search: MinHash + Jaccard

### Shingle Generation

Each symbol gets an enriched shingle set derived from five sources:

1. **Name parts** -- split camelCase/snake_case (`validateSession` -> `["validate", "session"]`)
2. **Parameter/return types** from signature (`(token string) (*User, error)` -> `["token", "string", "user", "error"]`)
3. **Top-20 body identifiers** by frequency (regex-extracted from ~50 lines of function body)
4. **Package name** tokens
5. **Doc comment keywords** (up to 4 lines above the symbol)

All shingles are lowercased and deduplicated.

### MinHash Signatures

128 independent hash functions via `hash/maphash` with different seeds. For each shingle set, computes a 128-element signature where each slot is the minimum hash value across all shingles for that hash function. Stored as BLOBs (128 * 8 bytes = 1KB per symbol).

### Search (current: brute-force)

Query string is shingled the same way, MinHashed, then compared against all stored signatures using Jaccard similarity (fraction of matching MinHash slots out of 128). Results above a 0.05 threshold are sorted descending, top-K returned.

**Performance**: sub-10ms for projects up to ~50k symbols. O(N) scan.

### What makes this different from grep

A search for "database connection pool" finds `initDBPool`, `pgxPoolConfig`, and `connectionManager` even though none match the exact query string. The enriched shingles capture semantic similarity through shared tokens, types, and referenced identifiers.

### What makes this different from embedding search

No API calls, no vector database, runs entirely offline. Not as good at pure semantic leaps ("auth" = "login" with zero shared tokens) but dramatically better than grep and costs nothing to run.

## Graph Queries

The `code_graph` tool accepts a symbol name, direction (`callers`/`callees`/`both`), and depth (1-3, currently only 1-hop implemented).

1. Keyword search via SQL `LIKE '%query%'` on symbol names (up to 5 matches)
2. For each match, look up incoming edges (`GetEdgesTo`) for callers, outgoing edges (`GetEdgesFrom`) for callees
3. Returns formatted listing with symbol kind, file, line, signature, and relationships

Go methods and interface methods are shown with their receiver,
`(*codegraph.Indexer).Build`, so same-named methods stay apart; a method
that satisfies interfaces gets an `Implements:` line, and a symbol from an
approximate file gets a note saying so.

## Code Smell Detection

`FindCodeSmells` (the `code_review` tool) runs one pass over every function and
method with its edge counts and source body and reports these kinds:

- `STUB`: a function with no outgoing calls (graph edges, or calls counted in
  its body) that is not a known leaf pattern such as a constructor or getter.
  This replaces the old `FindStubs` query.
- `LAZY_REDIRECT`: a function whose name implies work (an action verb) but
  which has at most two outgoing calls and redirects instead, for example by
  telling the user to use the CLI.
- `PLACEHOLDER`, `TODO_FIXME`, `EMPTY_HANDLER`, `HARDCODED`: text and shape
  checks on the body.

## LSH Banding (planned)

The MinHash signatures are already LSH-ready. The planned optimization partitions the 128 hash values into B bands of R rows (e.g., 16 bands x 8 rows), hashes each band into a bucket, and only compares symbols sharing a bucket with the query. This reduces search from O(N) brute-force to O(1) approximate lookup.

### Approach A: In-Memory Bands

`LSHIndex` struct with `map[uint64][]int64` per band. Built from existing `MinHashEntry` data at startup, queries narrow the candidate set before Jaccard ranking. ~50-80 lines on top of existing code.

### Approach B: Persisted Band Table

New `lsh_bands(band_id, band_hash, symbol_id)` SQLite table. Band hashes precomputed at index time. Query with `WHERE band_id = ? AND band_hash = ?`. O(1) lookup, scales to millions of symbols, but requires schema migration and more storage (~16 rows per symbol).

**Strategy**: implement A first, benchmark against brute-force across codebases of varying scale (281 -> 1,281 -> 1,774 files), promote to B if the performance gain justifies the complexity.

## Supported Symbol Kinds

`function`, `method`, `interface_method` (Go), `type`, `interface`, `struct`, `const`, `var`, `import`, `class`

## Supported Edge Kinds

- `calls`: every language. For Go: direct calls, calls through function
  values, fields, map/slice elements and factory results, and calls through
  interface methods.
- `references` (Go): the source takes the target as a value without calling
  it there.
- `implements` (Go): from an interface method to each module method that
  implements it.

Imports, embeds and type references are not tracked as edges. Outside Go
(and in approximate Go files) edges still resolve by bare symbol name, so
same-named functions in different packages can collapse onto one node.

## Supported Languages (indexable)

Go (go/types), Python (regex), JavaScript (regex), TypeScript (regex), Rust (regex)
