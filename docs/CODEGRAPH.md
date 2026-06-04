# Code Graph: Algorithms & Architecture

Technical reference for Celeste's code graph subsystem (`cmd/celeste/codegraph/`).

## Overview

The code graph provides structural understanding of codebases through three search layers:

| Layer | Example Query | What It Finds | When To Use |
|-------|--------------|---------------|-------------|
| **Graph** | "what calls X", "what implements Y" | Structural relationships | You know a specific symbol |
| **Semantic (MinHash)** | "code related to authentication" | Conceptually related symbols | You have a concept, not a name |
| **Keyword** | "validateSession" | Exact name matches | You know the exact name |

## Storage

SQLite (WAL mode) via `modernc.org/sqlite` (pure Go, no CGo). Three tables:

```sql
symbols (id, name, kind, package, file, line, signature, minhash BLOB)
edges   (source_id, target_id, kind)  -- directional, unique on (src, dst, kind)
files   (path, language, size, content_hash, indexed_at)
```

Indexed on `symbols.name`, `symbols.file`, `symbols.package`, `edges.source_id`, `edges.target_id`.

Database stored at `~/.celeste/projects/<sha256-prefix>/codegraph.db` to avoid polluting project directories.

## Parsing

Two strategies depending on language:

### Go (AST-based, deep fidelity)

Uses `go/ast` and `go/token`. Walks `FuncDecl` and `GenDecl` nodes to extract symbols (functions, methods, types, interfaces, structs, consts, vars, imports). Call edges extracted by `ast.Inspect` over function bodies, matching `*ast.CallExpr` nodes for both direct calls (`foo()`) and qualified calls (`pkg.Func()`).

### Other Languages (regex-based, broad coverage)

Covers Python, JavaScript, TypeScript, and Rust. Language-specific regex patterns extract declarations line-by-line (functions, classes, interfaces, imports, types, consts). Call edges use a `\b(\w+)\s*\(` heuristic -- matches any `identifier(` pattern, then filters to only known symbol names in the file. Keywords are excluded via a language-aware stop list.

Python class/method detection uses indentation tracking to distinguish top-level functions from methods inside classes.

### Tradeoff

Go gets real AST fidelity (accurate call graphs). Other languages get fast-but-approximate regex extraction with heuristic call detection. Adding tree-sitter would require CGo, which the project avoids.

## Indexing

### Full Build

Walks the file tree respecting `.gitignore` + a hardcoded skip list (`node_modules`, `vendor`, `venv`, `.git`, `dist`, `build`, `target`, etc.). For each indexable file: parse, store symbols, resolve edges, compute MinHash signatures, record file metadata.

### Incremental Updates

SHA-256 content hash per file. On update:
1. Deleted files: remove their symbols and edges
2. Changed files: delete old symbols, re-index
3. Unchanged files: skip

### Edge Resolution

Raw edges store symbol names (not IDs). Resolved at insert time:
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

## Code Smell Detection

Two structural heuristics built on the edge graph:

- **Stubs** (`FindStubs`): functions/methods with zero outgoing call edges. Candidates for dead code, placeholders, or trivially simple leaf functions.
- **Lazy redirect candidates** (`FindLazyRedirectCandidates`): functions with 0-2 outgoing edges that aren't known leaf patterns (constructors, getters). Candidates for further shingle/edge divergence analysis.

## LSH Banding (planned)

The MinHash signatures are already LSH-ready. The planned optimization partitions the 128 hash values into B bands of R rows (e.g., 16 bands x 8 rows), hashes each band into a bucket, and only compares symbols sharing a bucket with the query. This reduces search from O(N) brute-force to O(1) approximate lookup.

### Approach A: In-Memory Bands

`LSHIndex` struct with `map[uint64][]int64` per band. Built from existing `MinHashEntry` data at startup, queries narrow the candidate set before Jaccard ranking. ~50-80 lines on top of existing code.

### Approach B: Persisted Band Table

New `lsh_bands(band_id, band_hash, symbol_id)` SQLite table. Band hashes precomputed at index time. Query with `WHERE band_id = ? AND band_hash = ?`. O(1) lookup, scales to millions of symbols, but requires schema migration and more storage (~16 rows per symbol).

**Strategy**: implement A first, benchmark against brute-force across codebases of varying scale (281 -> 1,281 -> 1,774 files), promote to B if the performance gain justifies the complexity.

## Supported Symbol Kinds

`function`, `method`, `type`, `interface`, `struct`, `const`, `var`, `import`, `class`

## Supported Edge Kinds

`calls`, `imports`, `implements`, `embeds`, `references`

## Supported Languages (indexable)

Go (AST), Python (regex), JavaScript (regex), TypeScript (regex), Rust (regex)
