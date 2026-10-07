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
its last pass commits; it stamps `meta.graph_version` and `meta.edge_scope`
in the transaction that empties the graph, so the rows of an unfinished build are never taken for an
older index's. The Go pass does the same with
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
a reader waits for a writer's SQLite lock instead of failing. A file's
record is stored after its symbols, so a file whose symbols a power loss
dropped has no record or an old content hash, and the next update re-indexes
it. An update, finishing a build or not, skips every file whose content
hash matches its record, so an index whose file records survive
without their symbols (a symbol write that failed, or an index left
incomplete by a version without these marks) stays that way. Rebuild it with
`celeste index rebuild`, the MCP `celeste_index` tool's `rebuild` operation
or `/index rebuild` in the TUI (plain `celeste index` only updates). Three
tables:

```sql
symbols (id, name, kind, package, file, line, signature, decorators, base_classes,
         qual_name, implements, minhash BLOB)
edges   (source_id, target_id, kind)  -- directional, unique on (src, dst, kind)
files   (path, language, size, content_hash, indexed_at, resolution)
meta    (key, value)                  -- minhash_seeds, graph_version, edge_scope,
                                      -- go_modules, build_in_progress, go_pass_pending
```

Indexed on `symbols.name`, `symbols.file`, `symbols.package`, `symbols.qual_name`, `edges.source_id`, `edges.target_id`.

`qual_name` is the type-checked qualified name of a Go symbol, as go/types
prints it: `pkg/path.Func`, `(*pkg/path.T).Method`, `(pkg/path.Iface).Method`.
`implements` lists the interfaces a Go method satisfies (`error,fmt.Stringer`).
`files.resolution` is `typed` or `approximate` for Go files (see below).

`meta.graph_version` records the edge format. Version 2 is the type-checked
Go graph (#375). When an existing index has another version (or none),
`Update` turns it into an unfinished build of version 2 in one transaction:
it deletes the Go rows (symbols, files, BM25/LSH rows and every edge touching
a Go symbol), marks every other file for parsing again, sets
`meta.build_in_progress` and stamps the version and edge scope. It then finishes the build
as it finishes an interrupted one, so the other languages keep their rows
until each file is parsed again, every non-Go edge is resolved again, and the
result is the graph a fresh build gives (an index from v1.16 resolved each
file's calls before the later files were stored, so it lacks edges a build
has). Until the upgrade finishes, a re-parsed file's non-Go edges (into and
out of it) are missing: they return when the non-Go edges are resolved again
in one transaction. An update cut short part-way (a session closed during
indexing) keeps what it stored, and the next one carries on. Each run
type-checks the whole module before it stores any Go file, so a run must
outlast that type-check (and the non-Go re-resolve) to add Go rows; runs
shorter than that never finish the Go pass. An empty index gets a full
build. Go `init` functions are stored under
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
  `CGO_ENABLED=0` build, so its results do not depend on the host's C
  compiler: cgo-tagged files and files importing `"C"` take the fallback on
  every host. The import path comes from
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

A source build with `CGO_ENABLED=0` compiles the `parser_*_stub.go` files instead and uses the regex `GenericParser` below. So does a build with `CGO_ENABLED` and `CC` both unset on a machine whose default C compiler is missing, since Go then turns CGo off by itself; an explicit `CGO_ENABLED=1`, or a `CC` naming a compiler that is missing, fails the build instead. It has patterns for Python, JavaScript, TypeScript, Rust and PHP. Java, C, C++ and Ruby files are still indexed in that build, but only a generic `function`/`def`/`class` pattern applies to them, so they yield few symbols and edges. Language-specific regex patterns extract declarations line-by-line (functions, classes, interfaces, imports, types, consts). Call edges use a `\b(\w+)\s*\(` heuristic -- matches any `identifier(` pattern, then filters to only known symbol names in the file. Keywords are excluded via a language-aware stop list.

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
2. Fall back to global DB lookup by name, within the caller's language
   (TypeScript and JavaScript count as one, and so do C and C++, which share
   `.h` headers): a TypeScript call never resolves to a Python function
3. Strip qualifier prefix (`pkg.Func` -> `Func`) as a last resort

`meta.edge_scope` records that rule. An index built before it may hold edges
that cross languages, so the next `Update` resolves every non-Go edge again
and reruns the Go pass, once. It does so through the interrupted-build path:
in one transaction it sets `meta.build_in_progress` and stamps `edge_scope`
(no rows are deleted), then finishes the build as above and clears the mark.
On a large repository that first `Update` takes about as long as a full
build. If it is cancelled (for example by a tool deadline), the mark stays
set and the next `Update` finishes the work; nothing is lost in between, and
the old edges stay readable until the new ones replace them in one
transaction. Running `celeste index` once after upgrading does it up front.

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

The `code_graph` tool (`celeste_code_graph` over MCP) accepts a symbol name, direction (`callers`/`callees`/`both`), and depth (1-3, default 1; larger values are capped at 3). Direction is matched case-insensitively; an unknown one is an error.

1. Find the symbol (`LookupSymbol`), taking the first of these that matches:
   the exact name; a qualified name, in any form the tools print or a Go
   programmer writes (`(tui.AppModel).update`, `(*acp.session).update`,
   `AppModel.update`, `commands.Execute`, a full import path, and for other
   languages the file stem or path, `core.add`); the name ignoring case; a
   name that starts with or contains the query.
2. Every symbol of that tier is kept, non-test files first. Up to 8 are shown
   with their edges: incoming (`GetEdgesTo`) for callers, outgoing
   (`GetEdgesFrom`) for callees, walked breadth first up to `depth` hops:
   depth 2 adds callers of callers (or callees of callees), depth 3 one hop
   more. More than 8 same-named symbols, or several partial matches, are
   listed by qualified name, kind and `file:line` so the caller can query one
   of them.
3. Returns formatted listing with symbol kind, file, line, signature, and relationships

`code_search` in keyword mode uses the same ranking (exact and qualified
names first, then prefix and substring matches) and prints qualified names.

The first hop lists every edge of the queried symbol, in the same format a
one-hop query has always used. Later hops list each symbol once, at the hop
where it is first reached, never the queried symbol itself, and mark it with
the hop and the symbol it was reached through:

```
  Called by:
    <- middle (calls) main.go:5
    <- main (calls) main.go:3 [hop 2, via middle]
```

The first hop always lists every edge. Past the first hop, a direction stops
after 200 entries and says so.

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

The `kinds` argument is a comma-separated, case-insensitive list of these
kinds, or `ALL` (the default). An unknown kind is an error that lists the
valid ones, so a typo is never reported as a clean codebase.

## Queries Without an Index

The MCP query tools (`celeste_code_review`, `celeste_code_graph`,
`celeste_code_search`, `celeste_code_symbols`) never build an index. On a
workspace whose index was never built they return an error result
(`isError: true`) saying there is no code graph index and to run
`celeste_index` with `operation: "rebuild"` or `celeste index`, instead of an
empty answer that reads as "no findings" or "symbol not found". A query does
not create the index database either. While another indexer is building an
empty index they say it is being built, and when a full build was interrupted
(killed after it emptied the graph) they say the build did not finish and to
run `celeste_index` with `operation: "update"`. When the graph still holds rows
while `meta.build_in_progress` is set (an upgrade or edge-scope refresh, or a
build resumed part-way), they answer from it and put a note first: results
may be incomplete while the index is being updated, or, when no indexer is
running, the last update did not finish and `operation: "update"` finishes
it. They never suggest a rebuild for such an index, since the update keeps
its rows. Every soft tool error (a missing or
invalid argument, no index, an unknown background run) is an `isError`
result with the tool's message; JSON-RPC errors are kept for protocol faults.

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
same-named functions in different packages of one language can collapse onto
one node; they never cross languages.

## Supported Languages (indexable)

Go (go/types, every build). TypeScript/TSX, JavaScript, PHP, Python, Rust, Java, C, C++ (`.cpp`, `.cc`, `.cxx`, `.hpp`) and Ruby (tree-sitter in CGo builds, including release binaries). With `CGO_ENABLED=0` the same files go to the regex parser: Python, JavaScript, TypeScript, Rust and PHP have their own patterns; Java, C, C++ and Ruby get only the generic fallback.
