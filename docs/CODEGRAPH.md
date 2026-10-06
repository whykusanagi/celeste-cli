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

SQLite (WAL mode, `synchronous=NORMAL` and `foreign_keys` on every connection) via
`modernc.org/sqlite` (pure Go, no CGo). Index writes are autocommitted, about
a hundred per symbol; with `synchronous=NORMAL` a commit appends to the WAL
and the fsync waits for the next checkpoint, so a build does not pay a disk
flush per write (each one costs tens of milliseconds on Windows). The index is
derived data: a power loss can drop the last commits but cannot corrupt the
database, and commits are lost newest first. A full build sets
`meta.build_in_progress` before it empties the graph and clears it only after
its last pass commits, and the Go pass does the same with
`meta.go_pass_pending`. An `Update` that finds the first mark finishes the
build without resetting the graph: it keeps the files already indexed,
indexes the missing or changed ones, rewrites every non-Go edge (it
resolves them again, then deletes the old ones and stores the new ones in
one transaction), reruns the Go pass and only then clears the mark. The
graph is never emptied during recovery, and a file that did not change
never loses its edges: readers see its old edges until the new ones
commit. A missing or changed file is stored with its symbols first and
gets its edges only when that transaction commits, so until then a reader
sees it without edges, as during any update. Repeated short runs (a chat
opened and closed before a long build ends) each index more files and keep
what earlier runs stored. Rewriting the non-Go edges stops as soon as the
run is cancelled and then changes no edge, so closing a chat never waits
for it; the run that clears the mark is the first one that lasts through it
(`celeste index` does). An `Update` that finds the second mark reruns the
Go pass. A build or update that was
cancelled, killed or cut off by a power loss is finished by the next update
rather than trusted because its file hashes match. Each mark holds a
random token of the run that set it, and a run clears only its own.

Several indexers can open one database: the MCP server's, an MCP chat's, the
TUI's and `celeste index`. A build or update holds an exclusive OS file lock
on `codegraph.db.lock` next to the database (`flock` on Linux and macOS,
`LockFileEx` on Windows), so only one of them writes at a time. An update
that finds the lock taken skips, since the other run brings the index up to
date (the TUI, `celeste index` and the MCP `celeste_index` tool say so
instead of failing); an explicit build waits for it, up to two minutes, and
so does a rebuild or reset before it deletes the database files. A rebuild
keeps the lock until the new index is built, so no other indexer starts on
the deleted database in between. On Windows the database cannot be deleted
while another process (a TUI, another MCP server) has it open, even an idle
one: a rebuild then resets the graph and rebuilds it in place, and a reset
says the files are in use and deletes nothing. On Linux and macOS the delete
succeeds, and such a process keeps using the old, deleted file until it
reopens the index. The OS releases the
lock when its process exits or is killed, so a lock file left behind never
blocks the next indexer. Every connection also sets `busy_timeout` (10 s) so
a reader waits for a writer's SQLite lock instead of failing. An index left
incomplete by a version without these marks has nothing to repair it: rebuild
it with the MCP `celeste_index` tool's `rebuild` operation or `/index rebuild`
in the TUI (`celeste index` only updates). Three tables:

```sql
symbols (id, name, kind, package, file, line, signature, decorators, base_classes,
         qual_name, implements, minhash BLOB)
edges   (source_id, target_id, kind)  -- directional, unique on (src, dst, kind)
files   (path, language, size, content_hash, indexed_at, resolution)
meta    (key, value)                  -- minhash_seeds, graph_version, go_modules,
                                      -- build_in_progress, go_pass_pending
```

Indexed on `symbols.name`, `symbols.file`, `symbols.package`, `symbols.qual_name`, `edges.source_id`, `edges.target_id`.

`qual_name` is the type-checked qualified name of a Go symbol, as go/types
prints it: `pkg/path.Func`, `(*pkg/path.T).Method`, `(pkg/path.Iface).Method`.
`implements` lists the interfaces a Go method satisfies (`error,fmt.Stringer`).
`files.resolution` is `typed` or `approximate` for Go files (see below).

`meta.graph_version` records the edge format. Version 2 is the type-checked
Go graph (#375). When an existing index has another version (or none),
`Update` deletes only the Go rows (symbols, files, BM25/LSH rows and every
edge touching a Go symbol) in one transaction and re-runs the Go pass; other
languages keep their rows, because version 2 does not change them. The
version is stamped only when that update completes, so an update cancelled
part-way (a session closed during indexing) is redone by the next one. An
empty index gets a full build. Go `init` functions are stored under
`pkg/path.init#<file>`, one per file, since every `init` in a package shares
the name `pkg/path.init`.

Database stored at `~/.celeste/projects/<sha256-prefix>/codegraph.db` to avoid polluting project directories.

## Parsing

Three strategies depending on language and build:

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
  `GOTOOLCHAIN=local` so indexing never downloads anything, and with an
  explicit `-mod=readonly` (`-mod=vendor` for a vendored module) so a
  `GOFLAGS=-mod=mod` in the environment cannot rewrite the indexed `go.mod`.
  Without the `go` command, the standard library resolves only when the
  `GOROOT` environment variable points at a Go installation: release binaries
  are built with `-trimpath` and have no built-in GOROOT. Otherwise no import
  resolves and almost every file is approximate.
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

### Tree-sitter (AST-based, CGo builds)

TypeScript, PHP, Python, Rust, Java, C/C++ and Ruby (plus TSX and JavaScript through the TypeScript grammar) are parsed with tree-sitter grammars (`parser_multi_cgo.go`, `parser_ts_cgo.go`, node types in `parser_ts_languages.go`). Call edges come from the language's call-expression nodes, and Python decorators and base classes are recorded for the structural review. The grammars are C, so these files are behind `//go:build cgo`.

Release binaries are built with CGo on each platform's own runner and include all of these parsers. Every release build runs `celeste index selfcheck` before it ships: it indexes a small TypeScript/PHP/Python/Java repo and fails unless the tree-sitter results are in the graph. Which file extensions the indexer walks is set separately, by `indexableLanguages` in `detect.go`; every language above is in it.

### Regex fallback (CGO_ENABLED=0)

A source build with `CGO_ENABLED=0`, or on a machine without a C compiler, compiles the `parser_*_stub.go` files instead and uses the regex `GenericParser` below. It has patterns for Python, JavaScript, TypeScript, Rust and PHP. Java, C, C++ and Ruby files are still indexed in that build, but only a generic `function`/`def`/`class` pattern applies to them, so they yield few symbols and edges. Language-specific regex patterns extract declarations line-by-line (functions, classes, interfaces, imports, types, consts). Call edges use a `\b(\w+)\s*\(` heuristic -- matches any `identifier(` pattern, then filters to only known symbol names in the file. Keywords are excluded via a language-aware stop list.

Python class/method detection uses indentation tracking to distinguish top-level functions from methods inside classes.

### Tradeoff

Go gets type-checked call graphs (`go/types`) in every build. The tree-sitter languages get AST fidelity in release binaries and CGo source builds; a `CGO_ENABLED=0` build trades that for fast-but-approximate regex extraction with heuristic call detection, and needs no C toolchain.

## Indexing

### Full Build

Walks the file tree respecting `.gitignore` + a hardcoded skip list (`node_modules`, `vendor`, `venv`, `.git`, `dist`, `build`, `target`, etc.). For each indexable file: parse, store symbols, resolve edges, compute MinHash signatures, record file metadata. A full build starts from an empty graph (symbols, edges, files and BM25/LSH rows are cleared; MinHash seeds are kept), so search, `code_graph` and `code_review` see an empty or partial graph until it finishes. `Update` never empties the index, not even to finish a full build that was interrupted.

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

- `STUB`: a function with no callers whose body is a stub body. A stub body
  is exactly one of:
  - empty: no statements and no comments (`{}`, `pass`, `...`, a docstring);
  - TODO-only: no statements, and a comment with a `TODO`, `FIXME`, `XXX` or
    `HACK` marker;
  - not implemented: its only statements raise "not implemented"
    (`panic("not implemented")`, `raise NotImplementedError`,
    `throw new Error("not implemented")`, `UnsupportedOperationException`,
    `unimplemented!()`, `todo!()`).

  Any other statement makes the body real: a one-liner, a function that
  returns a literal (`true`, `false`, `nil`, `0`, `""`), a call. A function
  with callers, a declaration without a body (interface or abstract method),
  a comment-only body without a work marker (a documented no-op), an empty
  constructor, a Python dunder and a `Protocol`/`ABC`/`@abstractmethod`
  method are never STUBs.

  The reason says "likely dead code" only when nothing reaches the function
  implicitly. These are reached without a caller in the graph, and the
  reason names how instead: a constructor (Java and C++ constructors, Ruby
  `initialize`, PHP `__construct`, JS/TS `constructor`), Go `init` and a
  `main`, a test function (Go `TestXxx`/`BenchmarkXxx`/`FuzzXxx`/`ExampleXxx`
  in a `_test.go` file, `@Test`, a test-named function in a test file), a
  method that implements an interface or abstract method or overrides a
  base-class method (Go's type-checked `implements`, `@Override`, or the same
  name declared by another class while its own class extends or implements
  something; TS interface and abstract signatures and C++ pure virtual
  declarations count as declarations), the exported API of a library
  package (an exported Go function or method outside package `main`, an
  exported JS/TS function, a public method of a public Java class), and a
  function in a Go file with build constraints (a `//go:build` line or a
  GOOS/GOARCH file name suffix), whose callers are in another platform's
  build. A base-class method that only raises "not implemented" while
  subclasses override it is an abstract declaration, not a STUB.
- `LAZY_REDIRECT`: a function whose name implies work (an action verb) but
  which has at most two outgoing calls and redirects instead, for example by
  telling the user to use the CLI.
- `PLACEHOLDER`, `TODO_FIXME`, `EMPTY_HANDLER`, `HARDCODED`: text and shape
  checks on the body. `TODO_FIXME` and `HARDCODED` report the line the
  marker or value is on.

Each function's body is the span its parser recorded: go/ast for Go
(functions and methods), tree-sitter for TypeScript, JavaScript, PHP,
Python, Java, C, C++, Ruby and Rust in cgo builds. A nested function's lines
belong to the nested function. Without a parser span (a `CGO_ENABLED=0`
build) the body is found by a text scan from the definition line: braces,
Python indentation, or Ruby's matching `end`.

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
  it there, or converts to the target type (`Celsius(x)`).
- `implements` (Go): from an interface method to each module method that
  implements it.

Imports, embeds and type references other than conversions are not tracked
as edges. Outside Go
(and in approximate Go files) edges still resolve by bare symbol name, so
same-named functions in different packages can collapse onto one node.

## Supported Languages (indexable)

Go (go/types, every build). TypeScript/TSX, JavaScript, PHP, Python, Rust, Java, C, C++ (`.cpp`, `.cc`, `.cxx`, `.hpp`) and Ruby (tree-sitter in CGo builds, including release binaries). With `CGO_ENABLED=0` the same files go to the regex parser: Python, JavaScript, TypeScript, Rust and PHP have their own patterns; Java, C, C++ and Ruby get only the generic fallback.
