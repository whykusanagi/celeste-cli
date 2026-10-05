package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SearchResult pairs a symbol with its similarity score and a set of
// machine-readable reasoning fields that tell an LLM (or a human) WHY
// this result was returned and how confident celeste is in it.
//
// PathFlags: markers attached when the symbol's file path triggered the
// path-based post-filter — e.g. ["test"], ["mock", "generated"]. Clean-
// path results have an empty PathFlags slice. SemanticSearch demotes
// flagged results below clean results by default; see
// SemanticSearchOptions.ApplyPathFilter to disable.
//
// EdgeCount: total incoming + outgoing edges on this symbol in the code
// graph. A function that is called from 4 places and calls 2 others has
// EdgeCount=6. Zero-edge symbols are suspicious — they may be genuine
// dead code, but they may also be symbols the parser failed to resolve
// (especially TS/Python/Rust where the regex parser can't follow call
// sites through type definitions). SPEC §8.2 Issue #2 documents this
// ambiguity explicitly; LLMs should NOT treat EdgeCount=0 as proof of
// dead code without corroborating evidence.
//
// ConfidenceWarnings: human-readable strings describing caveats about
// this result. Derived at query time from PathFlags, EdgeCount, Kind,
// and Similarity — no schema change, no precomputation. Callers should
// surface these to whoever consumes the search results so low-quality
// matches are recognized as such instead of being treated as confident
// answers.
type SearchResult struct {
	Symbol     Symbol
	Similarity float64

	// BM25Score is the additive per-symbol BM25 score for this query,
	// computed alongside the Jaccard similarity at search time. Not a
	// replacement for Similarity — both signals are returned so callers
	// (or a downstream re-rank layer) can reason about them independently.
	// Zero when the BM25 corpus stats table is empty (pre-v1.9.0 index).
	BM25Score float64

	// MatchedTokens are the query tokens that appeared in this symbol's
	// filtered shingle set (intersection of query and symbol tokens).
	// Populated only when BM25 scoring is active. Useful reasoning output
	// for LLMs: "this result matched because it contains X, Y, Z".
	MatchedTokens []string

	PathFlags          []string
	EdgeCount          int
	ConfidenceWarnings []string
}

// Indexer manages the code graph lifecycle: build, update, and query.
type Indexer struct {
	workspace string
	store     *Store
	hasher    *MinHasher
	// tsParser is lazily initialized on the first .ts/.tsx file seen
	// during indexFile. Holding one long-lived parser and reusing it
	// across files avoids the native-allocation cost of a per-file
	// tree-sitter setup. Nil until first TS file; Close() releases it.
	tsParser *TSParser
	// multiParser handles all languages with tree-sitter grammars
	// (Python, Rust, Java, C, C++, etc). Lazily initialized.
	multiParser *MultiLangParser
	// buildMu serializes Build/BuildWithContext and Update/UpdateWithContext
	// against each other (and so also the lazy tsParser/multiParser creation
	// inside them): the TUI's /index rebuild|update calls these directly,
	// racing a nested run's refreshIndex, and the tree-sitter parsers are
	// not safe for concurrent use (2.0 F2e M6).
	buildMu sync.Mutex
	// token identifies the current Build or Update run in the marks it
	// sets (metaBuildInProgress, metaGoPassPending), so it clears only its
	// own. Set with the index lock held, under buildMu.
	token string
}

// DefaultIndexPath returns the path to the code graph database for a project.
// It stores the index under ~/.celeste/projects/<hash>/codegraph.db to avoid
// polluting the project directory, and creates that directory.
func DefaultIndexPath(projectRoot string) string {
	path := IndexPath(projectRoot)
	os.MkdirAll(filepath.Dir(path), 0755)
	return path
}

// IndexPath is DefaultIndexPath without creating the directory, for callers
// that only check whether an index exists (celeste_status).
func IndexPath(projectRoot string) string {
	homeDir, _ := os.UserHomeDir()
	hash := sha256.Sum256([]byte(projectRoot))
	hexHash := hex.EncodeToString(hash[:8]) // first 8 bytes = 16 hex chars
	return filepath.Join(homeDir, ".celeste", "projects", hexHash, "codegraph.db")
}

// NewIndexer creates an indexer for the given workspace, using the specified
// SQLite database path.
//
// Reloads the MinHasher seeds from the store's meta table if present so
// stored signatures remain comparable across process invocations. If no
// seeds are stored (fresh index or pre-v1.9.0 index), generates fresh
// random seeds that will be persisted on the first Build().
func NewIndexer(workspace, dbPath string) (*Indexer, error) {
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	hasher, err := loadOrInitHasher(store, DefaultNumHashes)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("load minhash seeds: %w", err)
	}

	return &Indexer{
		workspace: workspace,
		store:     store,
		hasher:    hasher,
	}, nil
}

// NewIndexerWithStore creates an indexer using an existing store.
// This is useful for testing where the store is set up manually.
// Unlike NewIndexer, does NOT attempt to load seeds from the store —
// the caller is responsible for passing a store that either has no
// meta row yet or whose seeds are irrelevant for the test.
func NewIndexerWithStore(store *Store, workspace string) *Indexer {
	hasher, _ := loadOrInitHasher(store, DefaultNumHashes)
	if hasher == nil {
		hasher = NewMinHasher(DefaultNumHashes)
	}
	return &Indexer{
		workspace: workspace,
		store:     store,
		hasher:    hasher,
	}
}

// loadOrInitHasher tries to reload the MinHash seeds from the store's
// meta table. If they exist and have the expected length, returns a
// MinHasher restored from those seeds (signatures will be comparable to
// anything previously stored against this DB). If they don't exist or
// are malformed, returns a fresh MinHasher whose seeds will be persisted
// on the next Build().
func loadOrInitHasher(store *Store, numHashes int) (*MinHasher, error) {
	blob, err := store.GetMeta("minhash_seeds")
	if err != nil {
		return nil, err
	}
	if blob == nil {
		// Fresh index or pre-v1.9.0 — generate new seeds now.
		// They get persisted in Build() via persistHasherSeeds.
		return NewMinHasher(numHashes), nil
	}
	seeds, err := BytesToSeeds(blob)
	if err != nil {
		// Corrupt blob — log? For now, fall through to fresh seeds.
		// The next Build() will overwrite the corrupt row.
		return NewMinHasher(numHashes), nil
	}
	if len(seeds) != numHashes {
		// Length mismatch (e.g. index was built with a different
		// DefaultNumHashes constant). Regenerate to match the
		// current constant. This invalidates any stored signatures,
		// but that's correct — they were computed at a different
		// signature length and can't be compared anyway.
		return NewMinHasher(numHashes), nil
	}
	return NewMinHasherFromSeeds(seeds), nil
}

// persistHasherSeeds stores the current MinHasher's seeds in the meta
// table. Idempotent and cheap — one SQLite upsert. Called at the end
// of Build() so a freshly-generated hasher's seeds are guaranteed to
// be recoverable on the next Open.
func (idx *Indexer) persistHasherSeeds() error {
	blob := SeedsToBytes(idx.hasher.Seeds())
	if err := idx.store.SetMeta("minhash_seeds", blob); err != nil {
		return fmt.Errorf("persist minhash seeds: %w", err)
	}
	return nil
}

// Close releases the underlying database connection and any native
// resources held by the tree-sitter TS parser.
func (idx *Indexer) Close() error {
	if idx.tsParser != nil {
		idx.tsParser.Close()
		idx.tsParser = nil
	}
	if idx.multiParser != nil {
		idx.multiParser.Close()
		idx.multiParser = nil
	}
	return idx.store.Close()
}

// Store returns the underlying store for direct queries (used by tools).
func (idx *Indexer) Store() *Store {
	return idx.store
}

// Build performs a full index of the workspace. Walks the file tree,
// parses source files, extracts symbols and edges, computes MinHash
// signatures, and stores everything in SQLite.
//
// Two-pass design (fix for issue #47): all symbols are stored in the first
// pass so that cross-file call edges can be resolved in the second pass
// regardless of file processing order. Without the two-pass approach, files
// processed alphabetically before their callee files (e.g. a.py calling
// in_databricks defined in util.py) would silently drop their edges because
// the target symbol didn't exist in the DB yet.
func (idx *Indexer) Build() error {
	return idx.BuildWithContext(context.Background())
}

// BuildWithContext is the cancellable variant of Build. It checks ctx between
// files so an index build started under a tool deadline can abort instead of
// walking the whole repo (task 349f1f14 complement).
func (idx *Indexer) BuildWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	idx.buildMu.Lock()
	defer idx.buildMu.Unlock()
	// Another Indexer on this database may be building or updating; wait
	// for it (bounded) rather than empty the graph under it (#392).
	lock, err := idx.acquireIndexLock(ctx, true)
	if err != nil {
		return err
	}
	defer lock.unlock()
	return idx.buildLocked(ctx)
}

// buildLocked is BuildWithContext with buildMu and the index lock held. A full build starts
// from an empty graph so rows from an older index (or an older index
// version) never linger next to the new ones. Readers see an empty or
// partial graph until it finishes; Update never empties the index, and
// finishes a build that was interrupted (metaBuildInProgress) in place.
func (idx *Indexer) buildLocked(ctx context.Context) error {
	files, err := idx.walkSourceFiles()
	if err != nil {
		return fmt.Errorf("walk files: %w", err)
	}
	// Mark the build unfinished before the graph is emptied; the mark is
	// cleared only after the last pass commits (#388).
	if err := idx.store.SetMeta(metaBuildInProgress, []byte(idx.token)); err != nil {
		return fmt.Errorf("mark build in progress: %w", err)
	}
	if err := idx.store.ResetGraph(); err != nil {
		return err
	}

	// Pass 1: store all symbols, MinHash, tokens, LSH bands, and file records.
	// Collect raw edges for deferred resolution in pass 2. Go files are
	// handled together afterwards by the type-checked Go pass.
	var allRawEdges []RawEdge
	var goFiles []string
	for i, path := range files {
		if i&63 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if DetectLanguage(path) == "go" {
			goFiles = append(goFiles, path)
			continue
		}
		raw, err := idx.indexFileSymbols(path)
		if err != nil {
			// Log but don't fail on individual file errors
			continue
		}
		allRawEdges = append(allRawEdges, raw...)
	}

	if testHookAfterPass1 != nil {
		testHookAfterPass1()
	}

	// Pass 2: resolve and store all edges now that every symbol is in the DB.
	// Cross-file call targets that weren't available during pass 1 are now
	// resolvable via GetSymbolIDByName.
	if err := ctx.Err(); err != nil {
		return err
	}
	idx.resolveAndStoreEdges(allRawEdges)

	if len(goFiles) > 0 {
		if err := idx.indexGo(ctx, goFiles, nil); err != nil {
			return err
		}
	} else if err := idx.store.DeleteMeta(metaGoPassPending); err != nil {
		// No Go pass ran, so a mark left by an earlier killed one is stale:
		// this run holds the index lock, so no live indexer owns it.
		return fmt.Errorf("clear go pass mark: %w", err)
	}

	// Persist the MinHasher seeds so a subsequent process can restore
	// the same hash family and compare signatures meaningfully. Idempotent
	// — re-running Build on the same index is a no-op for seeds because
	// loadOrInitHasher already restored them at NewIndexer time.
	if err := idx.persistHasherSeeds(); err != nil {
		return fmt.Errorf("persist seeds: %w", err)
	}

	// Rebuild the BM25 corpus statistics from the symbol_tokens rows
	// we just wrote. This is a single aggregation pass over the table
	// and produces the df/idf values used for BM25 scoring at query
	// time. Cheap compared to the indexing work we just finished.
	if _, err := idx.store.RebuildTokenStats(); err != nil {
		return fmt.Errorf("rebuild token stats: %w", err)
	}

	if err := idx.store.SetMeta(metaGraphVersion, []byte(graphVersion)); err != nil {
		return fmt.Errorf("record index version: %w", err)
	}
	if err := idx.store.DeleteMetaIf(metaBuildInProgress, idx.token); err != nil {
		return fmt.Errorf("mark build finished: %w", err)
	}
	return nil
}

// Update performs an incremental update. Only re-indexes files whose
// content hash has changed since the last index. Removes symbols for
// deleted files.
func (idx *Indexer) Update() error {
	return idx.UpdateWithContext(context.Background())
}

// UpdateWithContext is the cancellable variant of Update. It checks ctx between
// re-indexed files so an incremental update started under a tool deadline can
// abort instead of re-parsing the whole repo (task 349f1f14 complement).
func (idx *Indexer) UpdateWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	idx.buildMu.Lock()
	defer idx.buildMu.Unlock()
	// Another Indexer on this database is building or updating: skip
	// rather than race it (#392). Its run brings the index up to date.
	lock, err := idx.acquireIndexLock(ctx, false)
	if err != nil {
		return err
	}
	defer lock.unlock()
	return idx.updateLocked(ctx)
}

// updateLocked is UpdateWithContext with buildMu and the index lock held.
func (idx *Indexer) updateLocked(ctx context.Context) error {
	// A full build that never finished left file records whose hashes
	// match while edges are missing (#388). The graph is not emptied and
	// rebuilt: on a repo whose build outlasts each run (an Env closed
	// after a few seconds) that never finishes and shows readers an empty
	// graph every time (#391). The files already indexed are kept, the
	// loop below indexes the rest, every non-Go edge is resolved again and
	// the Go pass is rerun; the mark is cleared only when all of it has
	// committed.
	mark, err := idx.store.GetMeta(metaBuildInProgress)
	if err != nil {
		return err
	}
	recovering := mark != nil
	if recovering {
		// The run that set the mark is gone (this one holds the index
		// lock); take the mark over so this run can clear it.
		if err := idx.store.SetMeta(metaBuildInProgress, []byte(idx.token)); err != nil {
			return fmt.Errorf("mark build in progress: %w", err)
		}
	}
	// Likewise a Go pass that stopped part-way stored symbols and file
	// records but not (all of) the Go edges; rerun it.
	goPending, err := idx.store.GetMeta(metaGoPassPending)
	if err != nil {
		return err
	}
	indexedFiles, err := idx.store.GetAllFiles()
	if err != nil {
		return fmt.Errorf("get indexed files: %w", err)
	}
	// An index written by an older graph version (or never stamped) had its
	// Go edges resolved by name. Only the Go rows are dropped; the
	// incremental pass below then sees every Go file as new and re-runs the
	// Go pass, while other languages keep their rows. If this is cancelled
	// the version stays old and the next Update repeats it. An empty index
	// gets a full two-pass Build instead.
	if len(indexedFiles) == 0 {
		// Nothing to keep: an empty index (or a build cancelled before it
		// stored a file) gets the two-pass Build.
		return idx.buildLocked(ctx)
	}
	if v, err := idx.store.GetMeta(metaGraphVersion); err != nil || string(v) != graphVersion {
		if err := idx.store.ResetGo(); err != nil {
			return err
		}
		if indexedFiles, err = idx.store.GetAllFiles(); err != nil {
			return fmt.Errorf("get indexed files: %w", err)
		}
	}
	indexedMap := make(map[string]FileRecord)
	for _, f := range indexedFiles {
		indexedMap[f.Path] = f
	}

	// Walk current files
	currentFiles, err := idx.walkSourceFiles()
	if err != nil {
		return fmt.Errorf("walk files: %w", err)
	}
	currentSet := make(map[string]bool)
	for _, f := range currentFiles {
		currentSet[f] = true
	}

	// Delete symbols for removed files
	goRemoved := false
	for path := range indexedMap {
		if !currentSet[path] {
			if DetectLanguage(path) == "go" && !goRemoved {
				// Once the file record is gone nothing else says the Go
				// edges need rewriting, so mark the Go pass first.
				if err := idx.store.SetMeta(metaGoPassPending, []byte(idx.token)); err != nil {
					return err
				}
				goRemoved = true
			}
			_ = idx.store.DeleteFileSymbols(path)
			_ = idx.store.DeleteFile(path)
		}
	}

	// Index new or changed files. Changed Go files are collected and
	// re-indexed together by the Go pass, which needs whole packages. ctx
	// is checked every 64 re-indexed files (and every 1024 scanned ones),
	// so a cancelled run still stores a stride of new files and the next
	// one carries on from there.
	var goFiles []string
	goChanged := map[string]bool{}
	reindexed := 0
	// While recovering, the raw edges of the files this run re-indexes are
	// kept, so the re-resolve below does not parse them a second time.
	var recoveredEdges map[string][]RawEdge
	if recovering {
		recoveredEdges = map[string][]RawEdge{}
	}
	for i, path := range currentFiles {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		isGo := DetectLanguage(path) == "go"
		if isGo {
			goFiles = append(goFiles, path)
		}
		hash, err := fileContentHash(filepath.Join(idx.workspace, path))
		if err != nil {
			continue
		}
		if existing, ok := indexedMap[path]; ok && existing.ContentHash == hash {
			continue // unchanged
		}
		if isGo {
			goChanged[path] = true
			continue
		}
		if reindexed&63 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		reindexed++
		// Re-index this file. While recovering, its edges are resolved
		// with every other file's below, as a build's pass 2 does.
		_ = idx.store.DeleteFileSymbols(path)
		if recovering {
			if raw, err := idx.indexFileSymbols(path); err == nil {
				recoveredEdges[path] = raw
			}
			continue
		}
		if err := idx.indexFile(path); err != nil {
			continue
		}
	}
	if recovering {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := idx.reresolveNonGoEdges(ctx, currentFiles, recoveredEdges); err != nil {
			return err
		}
	}
	// A recovering update reruns the Go pass as the interrupted build would
	// have (without Go files, it clears a stale Go-pass mark as a build does).
	if (recovering && len(goFiles) > 0) || len(goChanged) > 0 || goRemoved || goPending != nil || (len(goFiles) > 0 && idx.goModulesChanged(goFiles)) {
		if err := idx.indexGo(ctx, goFiles, goChanged); err != nil {
			return err
		}
	} else if recovering {
		// Stale, as in buildLocked: this run holds the index lock.
		if err := idx.store.DeleteMeta(metaGoPassPending); err != nil {
			return fmt.Errorf("clear go pass mark: %w", err)
		}
	}

	// Persist the MinHasher seeds. Idempotent upsert — ensures the
	// seeds are written even on the `celeste index` CLI path which
	// calls Update() rather than Build(). Without this, fresh indexes
	// built via the CLI never persist their seeds and the meta.minhash_seeds
	// row stays missing, breaking cross-process signature reuse.
	if err := idx.persistHasherSeeds(); err != nil {
		return fmt.Errorf("persist seeds: %w", err)
	}

	// Rebuild BM25 corpus stats after an incremental update so df/idf
	// reflect the current symbol_tokens contents. Re-running is cheap
	// (single aggregation pass) and keeps scoring consistent even when
	// only a handful of files changed.
	if _, err := idx.store.RebuildTokenStats(); err != nil {
		return fmt.Errorf("rebuild token stats: %w", err)
	}

	if err := idx.store.SetMeta(metaGraphVersion, []byte(graphVersion)); err != nil {
		return fmt.Errorf("record index version: %w", err)
	}
	if recovering {
		if err := idx.store.DeleteMetaIf(metaBuildInProgress, idx.token); err != nil {
			return fmt.Errorf("mark build finished: %w", err)
		}
	}
	return nil
}

// reresolveNonGoEdges finishes an interrupted build's pass 2 without
// emptying the graph (#391): it collects every non-Go file's raw edges
// (from parsed, for the files this run already re-indexed, else by parsing
// the file again), deletes every edge that starts at a non-Go symbol and
// resolves the raw edges against the symbols now stored, as a build's pass
// 2 does. The Go pass rewrites Go-sourced edges itself.
//
// ctx is checked every 64 parsed files and every 1024 resolved edges, so a
// cancelled run (Env.Close) returns promptly. A cancel while parsing
// returns before any edge is deleted; a cancel while resolving leaves part
// of the non-Go edges stored, and the build_in_progress mark, still set,
// makes the next run do this again.
func (idx *Indexer) reresolveNonGoEdges(ctx context.Context, files []string, parsed map[string][]RawEdge) error {
	var raw []RawEdge
	n := 0
	for _, path := range files {
		if DetectLanguage(path) == "go" {
			continue
		}
		if edges, ok := parsed[path]; ok {
			raw = append(raw, edges...)
			continue
		}
		if n&63 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		n++
		if testHookReresolveParse != nil {
			testHookReresolveParse(path)
		}
		res, err := idx.parseFile(path)
		if err != nil || res == nil {
			continue
		}
		for i := range res.Edges {
			res.Edges[i].SourceFile = path
		}
		raw = append(raw, res.Edges...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := idx.store.DeleteNonGoEdges(); err != nil {
		return err
	}
	for start := 0; start < len(raw); start += 1024 {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx.resolveAndStoreEdges(raw[start:min(start+1024, len(raw))])
	}
	return nil
}

// indexFileSymbols parses a file, stores its symbols (with MinHash / LSH / tokens)
// and the file record, then returns the raw (unresolved) edges for deferred
// resolution. Used by Build() in its first pass so all symbols exist in the DB
// before any edges are inserted (fixing cross-file caller-count issue #47).
func (idx *Indexer) indexFileSymbols(relPath string) ([]RawEdge, error) {
	lang := DetectLanguage(relPath)
	result, err := idx.parseFile(relPath)
	if err != nil || result == nil {
		return nil, err
	}

	idx.storeFileSymbols(relPath, lang, result.Symbols)
	idx.storeFileRecord(relPath, lang, "")
	for i := range result.Edges {
		result.Edges[i].SourceFile = relPath
	}
	return result.Edges, nil
}

// storeFileSymbols stores a file's symbols with their MinHash signature,
// BM25 tokens and LSH bands, and returns name -> ID for the stored rows.
func (idx *Indexer) storeFileSymbols(relPath, lang string, syms []Symbol) map[string]int64 {
	source, _ := os.ReadFile(filepath.Join(idx.workspace, relPath))
	ids := make(map[string]int64, len(syms))
	for _, sym := range syms {
		sym.File = relPath
		id, err := idx.store.UpsertSymbol(sym)
		if err != nil {
			continue
		}
		if _, ok := ids[sym.Name]; !ok {
			ids[sym.Name] = id
		}
		if sym.Kind != SymbolImport {
			shingles := ShinglesForSymbol(sym, source, lang)
			sig := idx.hasher.Signature(shingles)
			_ = idx.store.UpdateMinHash(id, sig)
			// Persist per-symbol token frequencies so BM25 scoring has
			// something to read at query time. Same filtered shingle
			// set the MinHash saw — the two signals stay in lock-step
			// on what counts as a meaningful token for this symbol.
			_ = idx.store.UpsertSymbolTokens(id, shingles)
			// Compute and persist LSH band hashes so query-time search
			// can use the lsh_bands table instead of brute-force.
			// 64 bands × 2 elements from the 128-element signature.
			bands := ComputeBandHashes(sig)
			_ = idx.store.UpsertLSHBands(id, bands)
		}
	}
	return ids
}

// storeFileRecord records a file's size and content hash (and, for Go, its
// call-graph resolution) for incremental updates.
func (idx *Indexer) storeFileRecord(relPath, lang, resolution string) {
	absPath := filepath.Join(idx.workspace, relPath)
	info, _ := os.Stat(absPath)
	hash, _ := fileContentHash(absPath)
	var size int64
	if info != nil {
		size = info.Size()
	}
	_ = idx.store.UpsertFile(FileRecord{
		Path:        relPath,
		Language:    lang,
		Size:        size,
		ContentHash: hash,
		Resolution:  resolution,
	})
}

// resolveAndStoreEdges resolves raw (name-based) edges to symbol IDs and
// inserts them into the edges table. Called by Build() after indexFileSymbols
// has run over all files, ensuring every target symbol is already present.
func (idx *Indexer) resolveAndStoreEdges(edges []RawEdge) {
	for _, edge := range edges {
		sourceID, ok1 := idx.store.GetSymbolIDByNameInFile(edge.SourceName, edge.SourceFile)
		targetID, ok2 := idx.resolveTarget(edge.TargetName, edge.Kind, edge.SourceFile)
		// Try unqualified name: "pkg.Func" -> "Func"
		if !ok2 {
			if dotIdx := strings.LastIndex(edge.TargetName, "."); dotIdx >= 0 {
				unqualified := edge.TargetName[dotIdx+1:]
				targetID, ok2 = idx.resolveTarget(unqualified, edge.Kind, edge.SourceFile)
			}
		}
		if ok1 && ok2 {
			_ = idx.store.AddEdge(sourceID, targetID, edge.Kind)
		}
	}
}

// resolveTarget looks up an edge's target by name. A call resolves to a
// callable first and to one in the caller's file next, so a call to a
// common name such as get() does not land on an import or a type that
// happened to be stored first.
// A non-Go file's edge never targets a Go symbol (see GetCallableIDByName).
func (idx *Indexer) resolveTarget(name string, kind EdgeKind, fromFile string) (int64, bool) {
	if kind == EdgeCalls {
		return idx.store.GetCallableIDByName(name, fromFile)
	}
	return idx.store.GetSymbolIDByNameInFile(name, fromFile)
}

// indexFile parses a single file and stores its symbols, edges, and MinHash.
func (idx *Indexer) indexFile(relPath string) error {
	lang := DetectLanguage(relPath)
	result, err := idx.parseFile(relPath)
	if err != nil || result == nil {
		return err
	}

	symbolIDs := idx.storeFileSymbols(relPath, lang, result.Symbols)

	// Store edges (resolve names to IDs)
	// First try local file symbols, then fall back to global store lookup
	// for cross-file edges (e.g., calling functions from other packages).
	// For qualified names like "pkg.Func" or "obj.Method", also try the
	// unqualified suffix (just "Func" or "Method") since symbols are stored
	// without receiver/package prefixes.
	for _, edge := range result.Edges {
		sourceID, ok1 := symbolIDs[edge.SourceName]
		if !ok1 {
			sourceID, ok1 = idx.store.GetSymbolIDByName(edge.SourceName)
		}
		// The target goes through the store rather than symbolIDs: this
		// file's symbols are already stored, and resolveTarget prefers
		// them, but it also prefers a callable over a same-named import.
		targetID, ok2 := idx.resolveTarget(edge.TargetName, edge.Kind, relPath)
		// Try unqualified name: "pkg.Func" -> "Func"
		if !ok2 {
			if dotIdx := strings.LastIndex(edge.TargetName, "."); dotIdx >= 0 {
				unqualified := edge.TargetName[dotIdx+1:]
				targetID, ok2 = idx.resolveTarget(unqualified, edge.Kind, relPath)
			}
		}
		if ok1 && ok2 {
			_ = idx.store.AddEdge(sourceID, targetID, edge.Kind)
		}
	}

	idx.storeFileRecord(relPath, lang, "")

	return nil
}

// parseFile parses one workspace file with the parser for its language. It
// returns a nil result for a language without a parser.
func (idx *Indexer) parseFile(relPath string) (*ParseResult, error) {
	absPath := filepath.Join(idx.workspace, relPath)
	lang := DetectLanguage(relPath)

	switch {
	case lang == "go":
		// Go uses its own AST parser (go/parser, not tree-sitter)
		return NewGoParser().ParseFile(absPath)
	case idx.tryMultiParser(absPath):
		// Multi-language tree-sitter parser (Python, Rust, Java, C, C++, etc.)
		if idx.multiParser == nil {
			idx.multiParser = NewMultiLangParser()
		}
		return idx.multiParser.ParseFile(absPath)
	case lang == "typescript":
		// Dedicated TS parser (preserves existing behavior for TS-only builds)
		if idx.tsParser == nil {
			idx.tsParser = NewTSParser()
		}
		return idx.tsParser.ParseFile(absPath)
	case indexableLanguages[lang]:
		return NewGenericParser(lang).ParseFile(absPath)
	}
	return nil, nil // no parser for this language
}

// walkSourceFiles returns relative paths of all indexable source files.
func (idx *Indexer) walkSourceFiles() ([]string, error) {
	var files []string

	gitignore := LoadGitignore(idx.workspace)

	err := filepath.WalkDir(idx.workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errored entries
		}

		rel, err := filepath.Rel(idx.workspace, path)
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if ShouldSkipPath(rel) {
				return filepath.SkipDir
			}
			if gitignore.ShouldSkip(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}

		if ShouldSkipPath(rel) {
			return nil
		}
		if gitignore.ShouldSkip(rel, false) {
			return nil
		}

		if IsIndexableFile(d.Name()) {
			files = append(files, rel)
		}

		return nil
	})

	return files, err
}

// SemanticSearchOptions configures SemanticSearch behavior. Existing
// callers of SemanticSearch(query, topK) get the default behavior —
// path filter ON, structural rerank ON — without any changes.
type SemanticSearchOptions struct {
	// TopK is the maximum number of results to return. Required.
	TopK int

	// MinSimilarity is the Jaccard floor below which results are dropped
	// entirely. Zero means use the default (0.05).
	MinSimilarity float64

	// ApplyPathFilter, when true, demotes results whose file path matches
	// a known "noisy" pattern (test/mock/generated/vendored/declaration)
	// below clean-path results. Default when using SemanticSearch is true.
	// Set false for raw unfiltered results.
	ApplyPathFilter bool

	// Reranker, when non-nil, is applied to the candidate list after
	// the Jaccard + BM25 fusion and before the path filter tiering.
	// A pluggable seam — the default (set via SemanticSearch) is
	// StructuralReranker which does pure-Go feature-based rescoring.
	// Future cloud/local embedding rerankers can implement this
	// interface without touching the search pipeline.
	//
	// Pass a zero value (nil) together with
	// DisableRerank=true to get the pre-Task-24 behavior (fusion-only).
	Reranker Reranker

	// DisableRerank bypasses the Reranker even if one is set.
	// Useful for A/B testing and for callers that want the raw
	// fused ordering without any structural adjustments.
	DisableRerank bool
}

// SemanticSearch finds symbols semantically similar to the query string.
// The query is split into shingles, MinHashed, then compared against all
// symbol signatures using brute-force Jaccard similarity.
//
// Applies the path-based post-filter by default — test/mock/generated/
// vendored/declaration results are partitioned below clean-path results
// of comparable similarity. Use SemanticSearchWithOptions to disable.
func (idx *Indexer) SemanticSearch(query string, topK int) ([]SearchResult, error) {
	return idx.SemanticSearchWithOptions(query, SemanticSearchOptions{
		TopK:            topK,
		ApplyPathFilter: true,
		Reranker:        NewStructuralReranker(),
	})
}

// SemanticSearchWithOptions is the full-options variant of SemanticSearch.
func (idx *Indexer) SemanticSearchWithOptions(query string, opts SemanticSearchOptions) ([]SearchResult, error) {
	return idx.SemanticSearchWithContext(context.Background(), query, opts)
}

// SemanticSearchWithContext is the cancellable variant. It checks ctx before the
// expensive candidate-scoring loops so a search invoked as a tool can be aborted
// at the agent's tool deadline instead of spinning the corpus to completion
// (task 349f1f14 complement — stops the abandoned goroutine's CPU burn).
func (idx *Indexer) SemanticSearchWithContext(ctx context.Context, query string, opts SemanticSearchOptions) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.TopK <= 0 {
		opts.TopK = 10
	}
	minSim := opts.MinSimilarity
	if minSim == 0 {
		minSim = 0.05
	}

	// Generate query shingles from the query string
	words := strings.Fields(strings.ToLower(query))
	var queryShingles []string
	for _, w := range words {
		queryShingles = append(queryShingles, splitIdentifier(w)...)
	}
	queryShingles = deduplicateLowercase(queryShingles)

	// Apply stop-word filter to the query side. This is SPEC §6.2
	// application point 2: the query tokenization must pass through
	// the same filter that the symbol shingle sets went through at
	// index time, otherwise a stop-worded token in the symbol set
	// would still match against a non-stop-worded token in the query
	// and vice versa, producing asymmetric Jaccard scores.
	//
	// Queries aren't language-tagged (a free-form "database connection
	// pool query" could target Go, Python, TS, or any mix), so we pass
	// "" for lang and apply only the universal set.
	if stopWords != nil {
		queryShingles = stopWords.Filter(queryShingles, "")
	}

	querySig := idx.hasher.Signature(queryShingles)

	// Candidate retrieval: use LSH band lookup when available,
	// fall back to brute-force for pre-LSH indexes. The LSH path
	// queries the lsh_bands table for symbols sharing at least one
	// band hash with the query — typically 0.1-1% of the corpus —
	// then fetches only those candidates' MinHash signatures for
	// exact Jaccard ranking. At grafana scale (77K symbols) this
	// provides a ~20x speedup over the brute-force path.
	type scored struct {
		symbolID   int64
		similarity float64
	}
	var results []scored

	usedLSH := false
	if idx.store.HasLSHData() {
		// LSH path: compute query band hashes → candidate set → Jaccard rank.
		queryBands := ComputeBandHashes(querySig)
		candidateIDs, err := idx.store.QueryLSHCandidates(queryBands)
		if err != nil {
			return nil, fmt.Errorf("lsh candidates: %w", err)
		}
		if len(candidateIDs) > 0 {
			usedLSH = true
			for i, id := range candidateIDs {
				if i&511 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				sig, err := idx.store.GetMinHash(id)
				if err != nil || sig == nil {
					continue
				}
				sim := JaccardSimilarity(querySig, sig)
				if sim > minSim {
					results = append(results, scored{id, sim})
				}
			}
		}
		// If LSH returned 0 candidates (can happen on very small corpora
		// where the 64×2 band hash space is too sparse for any collision),
		// fall through to brute-force below so the query still produces
		// results. LSH provides no speedup on tiny indexes anyway.
	}
	if !usedLSH {
		// Brute-force: load all signatures, compare exhaustively. Used for
		// pre-LSH indexes AND as a safety net when LSH returns no candidates.
		entries, err := idx.store.GetAllMinHashes()
		if err != nil {
			return nil, fmt.Errorf("get minhashes: %w", err)
		}
		for i, entry := range entries {
			if i&511 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			sim := JaccardSimilarity(querySig, entry.Signature)
			if sim > minSim {
				results = append(results, scored{entry.SymbolID, sim})
			}
		}
	}

	// Sort by similarity descending — this is the initial raw ranking
	// before any path-based demotion.
	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	// Widen the candidate pool when path filtering is on. We need more
	// than topK candidates so we can drop noisy ones and still return
	// topK clean matches. Pull up to 3x topK candidates, capped at the
	// full result set.
	candidateLimit := opts.TopK
	if opts.ApplyPathFilter {
		candidateLimit = opts.TopK * 3
	}
	if len(results) > candidateLimit {
		results = results[:candidateLimit]
	}

	// Resolve symbol details and classify paths for each candidate.
	// Also compute edge counts and confidence warnings so the caller
	// has machine-readable reasoning for every result. Each candidate
	// becomes a fully-annotated SearchResult.
	//
	// BM25 scoring: reads one-time corpus stats + IDF table, then scores
	// each candidate against the query tokens using the symbol's stored
	// TF map. Pre-v1.9.0 indexes have empty token_stats — scoring then
	// degenerates to zero across the board and the fused ranking reduces
	// to pure Jaccard, which is the correct fallback.
	bm25Stats, _ := idx.store.ReadBM25Stats()
	var idfMap map[string]float64
	if bm25Stats != nil && bm25Stats.NumDocs > 0 {
		idfMap, _ = idx.store.GetIDFs(queryShingles)
	}

	resolutions, _ := idx.store.FileResolutions()

	var allCandidates []SearchResult
	for _, r := range results {
		sym, err := idx.store.GetSymbol(r.symbolID)
		if err != nil {
			continue
		}
		flags := ClassifyPath(sym.File)

		// Cheap edge-count lookup. Both GetEdgesFrom and GetEdgesTo
		// already have covering SQL indexes (idx_edges_source / _target)
		// so these are O(log N) lookups with tiny result sets. For the
		// ~30 candidates we process per query the cost is negligible.
		edgesOut, _ := idx.store.GetEdgesFrom(r.symbolID)
		edgesIn, _ := idx.store.GetEdgesTo(r.symbolID)
		edgeCount := len(edgesOut) + len(edgesIn)

		warnings := computeConfidenceWarnings(*sym, r.similarity, flags, edgeCount)
		if resolutions[sym.File] == GoResolutionApproximate {
			warnings = append(warnings, WarnApproximateGraph)
		}

		// Per-candidate BM25 score + matched-token list. Only computed
		// if the corpus stats exist (otherwise we'd do pointless table
		// reads for zero output). MatchedTokens is derived from the
		// intersection of queryShingles and the symbol's TF map so it
		// stays in sync with whatever actually contributed to the score.
		var bm25Score float64
		var matched []string
		if bm25Stats != nil && bm25Stats.NumDocs > 0 {
			docTokens, docLen, tokErr := idx.store.GetSymbolTokens(r.symbolID)
			if tokErr == nil && docLen > 0 {
				bm25Score = ComputeBM25Score(queryShingles, docTokens, docLen, idfMap, bm25Stats.AvgDocLength)
				for _, qt := range queryShingles {
					if _, ok := docTokens[qt]; ok {
						matched = append(matched, qt)
					}
				}
			}
		}

		allCandidates = append(allCandidates, SearchResult{
			Symbol:             *sym,
			Similarity:         r.similarity,
			BM25Score:          bm25Score,
			MatchedTokens:      matched,
			PathFlags:          PathFlagStrings(flags),
			EdgeCount:          edgeCount,
			ConfidenceWarnings: warnings,
		})
	}

	// Rank-fuse Jaccard and BM25 using Reciprocal Rank Fusion. This is
	// the point where the two signals merge into a single ordering. Both
	// raw scores stay on each SearchResult so callers can audit; only
	// the slice order reflects the fused view. If BM25 is disabled
	// (empty corpus stats) every BM25Score is 0, the BM25 rank map is
	// a flat tie, and RRF gracefully degrades toward the Jaccard ranking.
	if len(allCandidates) > 1 {
		jaccardRanks := make(map[int64]int, len(allCandidates))
		bm25Ranked := make([]SearchResult, len(allCandidates))
		copy(bm25Ranked, allCandidates)
		for i, c := range allCandidates {
			jaccardRanks[c.Symbol.ID] = i + 1
		}
		sort.SliceStable(bm25Ranked, func(i, j int) bool {
			return bm25Ranked[i].BM25Score > bm25Ranked[j].BM25Score
		})
		bm25Ranks := make(map[int64]int, len(bm25Ranked))
		for i, c := range bm25Ranked {
			bm25Ranks[c.Symbol.ID] = i + 1
		}
		fusedOrder := ComputeFusedRanking(jaccardRanks, bm25Ranks)
		byID := make(map[int64]SearchResult, len(allCandidates))
		for _, c := range allCandidates {
			byID[c.Symbol.ID] = c
		}
		fused := make([]SearchResult, 0, len(allCandidates))
		for _, id := range fusedOrder {
			if c, ok := byID[id]; ok {
				fused = append(fused, c)
			}
		}
		allCandidates = fused
	}

	// Structural rerank. Applied after the Jaccard+BM25 fusion and
	// before the path-filter tier partitioning so that rerank
	// adjustments (matched-token-ratio boost, edge-density boost,
	// zero-edge penalty) reorder candidates within each would-be tier
	// without reshuffling clean-vs-demoted boundaries. Skipped when
	// DisableRerank is set or when no Reranker is installed — then
	// the caller gets the raw fused ordering.
	if !opts.DisableRerank && opts.Reranker != nil && len(allCandidates) > 1 {
		allCandidates = opts.Reranker.Rerank(allCandidates, len(queryShingles))
	}

	if !opts.ApplyPathFilter {
		// No path filter — just truncate to topK and return.
		if len(allCandidates) > opts.TopK {
			allCandidates = allCandidates[:opts.TopK]
		}
		return allCandidates, nil
	}

	// Path filter enabled. If the query itself is asking for test/mock
	// code, do not demote — respect user intent.
	if queryWantsTests(query) {
		if len(allCandidates) > opts.TopK {
			allCandidates = allCandidates[:opts.TopK]
		}
		return allCandidates, nil
	}

	// Partition into clean tier and demoted tier, preserving within-tier
	// order (which is already similarity descending from the sort above).
	clean := make([]SearchResult, 0, len(allCandidates))
	demoted := make([]SearchResult, 0, len(allCandidates))
	for _, r := range allCandidates {
		if len(r.PathFlags) == 0 {
			clean = append(clean, r)
		} else {
			demoted = append(demoted, r)
		}
	}

	// Concatenate: clean first, demoted after, truncate to topK. This
	// means if there are >= topK clean results, demoted results never
	// appear — the LLM/user sees only high-confidence production code.
	// If clean runs short, demoted results fill the remaining slots so
	// the caller still gets useful fallback options on sparse corpora.
	final := make([]SearchResult, 0, opts.TopK)
	for _, r := range clean {
		if len(final) >= opts.TopK {
			break
		}
		final = append(final, r)
	}
	for _, r := range demoted {
		if len(final) >= opts.TopK {
			break
		}
		final = append(final, r)
	}
	return final, nil
}

// KeywordSearch finds symbols matching a keyword query using SQL LIKE.
func (idx *Indexer) KeywordSearch(query string, limit int) ([]Symbol, error) {
	syms, err := idx.store.SearchSymbolsByName(query)
	if err != nil {
		return nil, err
	}
	if len(syms) > limit {
		syms = syms[:limit]
	}
	return syms, nil
}

// Stats returns aggregate stats for the indexed codebase.
func (idx *Indexer) Stats() (*StoreStats, error) {
	return idx.store.Stats()
}

// ProjectSummary returns a brief summary suitable for the system prompt.
func (idx *Indexer) ProjectSummary() string {
	stats, err := idx.store.Stats()
	if err != nil {
		return ""
	}

	// Detect project name from go.mod or directory name
	projectName := filepath.Base(idx.workspace)
	modPath := filepath.Join(idx.workspace, "go.mod")
	if data, err := os.ReadFile(modPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					// Use last path component
					modParts := strings.Split(parts[1], "/")
					projectName = modParts[len(modParts)-1]
				}
				break
			}
		}
	}

	lang := DetectProjectLanguage(idx.workspace)
	if lang == "" {
		lang = "mixed"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s (%s)\n", projectName, lang)
	fmt.Fprintf(&b, "Files: %d | Symbols: %d | Edges: %d\n",
		stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)

	// Top packages by symbol count
	if len(stats.SymbolsByKind) > 0 {
		var kinds []string
		for kind, count := range stats.SymbolsByKind {
			kinds = append(kinds, fmt.Sprintf("%s: %d", kind, count))
		}
		sort.Strings(kinds)
		fmt.Fprintf(&b, "Symbols: %s\n", strings.Join(kinds, ", "))
	}

	if res, err := idx.store.FileResolutions(); err == nil && len(res) > 0 {
		typed := 0
		for _, r := range res {
			if r == GoResolutionTyped {
				typed++
			}
		}
		fmt.Fprintf(&b, "Go call graph: %d files type-checked, %d approximate\n", typed, len(res)-typed)
	}

	return b.String()
}

// actionVerbs are name components that imply a function should DO work beyond
// just building/formatting strings. Builder/formatter functions are excluded
// because being string-heavy IS their purpose.
var actionVerbs = map[string]bool{
	"handle": true, "execute": true, "run": true, "process": true,
	"perform": true, "dispatch": true, "invoke": true, "apply": true,
	"show": true, "display": true,
	"fetch": true, "load": true, "save": true, "store": true,
	"send": true, "publish": true, "emit": true, "broadcast": true,
	"validate": true, "check": true, "verify": true, "analyze": true,
	"sync": true, "update": true, "refresh": true, "register": true,
	"deploy": true, "install": true, "setup": true, "configure": true,
}

// builderPrefixes are name prefixes for functions whose purpose IS to build
// strings, templates, or data structures — being string-heavy is expected.
var builderPrefixes = []string{
	"build", "format", "render", "create", "generate", "compose",
	"compute", "calculate", "transform", "convert", "parse", "marshal",
	"encode", "decode", "serialize", "template", "compile", "assemble",
}

// isTestFilePath returns true if the file path looks like a test file.
func isTestFilePath(file string) bool {
	// Suffix-based test files.
	if strings.HasSuffix(file, "_test.go") ||
		strings.HasSuffix(file, "_test.py") ||
		strings.HasSuffix(file, ".test.ts") ||
		strings.HasSuffix(file, ".test.js") ||
		strings.HasSuffix(file, ".spec.ts") ||
		strings.HasSuffix(file, ".spec.js") {
		return true
	}
	// Python conventions.
	base := path.Base(file)
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	if base == "conftest.py" {
		return true
	}
	// "test"/"tests" as a path segment at ANY depth, including top-level.
	for _, seg := range strings.Split(file, "/") {
		if seg == "test" || seg == "tests" {
			return true
		}
	}
	return false
}

// isExpectedLeaf returns true if a function name matches patterns that are
// expected to have few/zero outgoing edges (not lazy redirects).
func isExpectedLeaf(name string) bool {
	// Interface implementations and trivial methods
	leafNames := map[string]bool{
		"Close": true, "Init": true, "String": true, "Error": true,
		"Len": true, "Name": true, "Description": true, "Parameters": true,
		"IsReadOnly": true, "ValidateInput": true, "InterruptBehavior": true,
		"Less": true, "Swap": true, "MarshalJSON": true, "UnmarshalJSON": true,
		"Reset": true, "ProtoMessage": true, "View": true, "IsConcurrencySafe": true,
	}
	if leafNames[name] {
		return true
	}

	// Prefix patterns for constructors, accessors
	prefixes := []string{"New", "Get", "Set", "Is", "Has", "Test", "Benchmark", "With"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return true
		}
	}

	return false
}

// CodeSmellKind categorizes the type of code smell detected.
type CodeSmellKind string

const (
	SmellLazyRedirect CodeSmellKind = "LAZY_REDIRECT"
	SmellStub         CodeSmellKind = "STUB"
	SmellPlaceholder  CodeSmellKind = "PLACEHOLDER"
	SmellTodoFixme    CodeSmellKind = "TODO_FIXME"
	SmellEmptyHandler CodeSmellKind = "EMPTY_HANDLER"
	SmellHardcoded    CodeSmellKind = "HARDCODED"
)

// CodeSmell represents a structurally detected code issue.
type CodeSmell struct {
	Kind      CodeSmellKind `json:"kind"`
	Name      string        `json:"name"`
	File      string        `json:"file"`
	Line      int           `json:"line"`
	FuncKind  string        `json:"func_kind"`
	OutEdges  int           `json:"outgoing_edges"`
	InEdges   int           `json:"incoming_edges"`
	Score     float64       `json:"score"`
	Reason    string        `json:"reason"`
	Signature string        `json:"signature,omitempty"`
	Snippet   string        `json:"snippet,omitempty"`
}

// FindCodeSmells performs a single-pass structural analysis over all functions
// in the graph, detecting multiple code smell patterns simultaneously.
// This is more efficient than separate queries and more powerful than grep
// because it combines graph structure (edges, connectivity) with body analysis.
func (idx *Indexer) FindCodeSmells(kinds []CodeSmellKind, maxResults int, includeTests bool) ([]CodeSmell, error) {
	// Build a set of requested kinds for fast lookup
	wantKind := make(map[CodeSmellKind]bool)
	for _, k := range kinds {
		wantKind[k] = true
	}
	wantAll := len(kinds) == 0 || wantKind["ALL"]

	// Get all functions/methods with their edge counts
	candidates, err := idx.store.FindAllFunctionsWithEdges()
	if err != nil {
		return nil, err
	}

	var results []CodeSmell

	// Cache file reads — many symbols share the same file
	fileCache := make(map[string][]byte)

	for _, c := range candidates {
		if !includeTests && isTestFilePath(c.File) {
			continue
		}

		// Read source file (cached)
		absFile := c.File
		if !filepath.IsAbs(absFile) {
			absFile = filepath.Join(idx.workspace, absFile)
		}
		sourceData, cached := fileCache[absFile]
		if !cached {
			data, err := os.ReadFile(absFile)
			if err != nil {
				// STUB detection still works without source (graph-only)
				if wantAll || wantKind[SmellStub] {
					if c.OutEdges == 0 && !isExpectedLeaf(c.Name) {
						if smell, ok := detectStub(c, 0, nil); ok {
							results = append(results, smell)
						}
					}
				}
				continue
			}
			sourceData = data
			fileCache[absFile] = sourceData
		}

		sym := Symbol{Name: c.Name, Line: c.Line}
		// Use scoped body extraction to prevent bleed into adjacent functions
		body := findScopedBody(sourceData, sym)
		lowerBody := strings.ToLower(body)
		bodyLines := strings.Split(strings.TrimSpace(body), "\n")

		// Count actual calls in body (source-level, independent of graph edges)
		bodyCalls := countBodyCalls(body, c.Name)

		// Effective outgoing edge count: use graph edges if available,
		// otherwise fall back to body call count
		effectiveOut := c.OutEdges
		if effectiveOut == 0 {
			effectiveOut = bodyCalls
		}

		// --- STUB detection ---
		if wantAll || wantKind[SmellStub] {
			if effectiveOut == 0 && !isExpectedLeaf(c.Name) {
				if smell, ok := detectStub(c, bodyCalls, bodyLines); ok {
					results = append(results, smell)
				}
			}
		}

		// --- LAZY REDIRECT detection ---
		if wantAll || wantKind[SmellLazyRedirect] {
			if effectiveOut <= 2 && !isExpectedLeaf(c.Name) {
				if smell, ok := detectLazyRedirect(c, body, lowerBody, sourceData); ok {
					results = append(results, smell)
				}
			}
		}

		// --- PLACEHOLDER detection ---
		if wantAll || wantKind[SmellPlaceholder] {
			if smell, ok := detectPlaceholder(c, body, lowerBody, bodyLines); ok {
				results = append(results, smell)
			}
		}

		// --- TODO/FIXME detection ---
		if wantAll || wantKind[SmellTodoFixme] {
			if smells := detectTodoFixme(c, body, bodyLines); len(smells) > 0 {
				results = append(results, smells...)
			}
		}

		// --- EMPTY HANDLER detection ---
		if wantAll || wantKind[SmellEmptyHandler] {
			if smell, ok := detectEmptyHandler(c, body, lowerBody); ok {
				results = append(results, smell)
			}
		}

		// --- HARDCODED detection ---
		if wantAll || wantKind[SmellHardcoded] {
			if smells := detectHardcoded(c, body, bodyLines); len(smells) > 0 {
				results = append(results, smells...)
			}
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// FunctionEdgeInfo holds a function's identity and edge counts for analysis.
type FunctionEdgeInfo struct {
	Name        string
	File        string
	Line        int
	Kind        string
	Signature   string
	Decorators  string // comma-separated decorator names captured at parse time
	BaseClasses string // comma-separated base-class names of the enclosing class
	Implements  string // Go: interfaces this method satisfies (symbols.implements)
	Resolution  string // Go: files.resolution of the symbol's file
	OutEdges    int
	InEdges     int
}

func detectLazyRedirect(c FunctionEdgeInfo, body, lowerBody string, sourceData []byte) (CodeSmell, bool) {
	nameParts := splitIdentifier(c.Name)

	// Exclude builder/formatter functions — being string-heavy IS their purpose
	for _, prefix := range builderPrefixes {
		if strings.HasPrefix(strings.ToLower(c.Name), prefix) {
			return CodeSmell{}, false
		}
	}

	// Exclude registration, detection, and code analysis tool functions
	if strings.HasPrefix(c.Name, "Register") || strings.HasPrefix(c.Name, "detect") ||
		strings.HasPrefix(c.Name, "NewCode") {
		return CodeSmell{}, false
	}
	// Skip functions in code analysis tool files (contain patterns as string data)
	if strings.Contains(c.File, "code_review") || strings.Contains(c.File, "code_smells") ||
		strings.Contains(c.File, "code_stubs") || strings.Contains(c.File, "code_lazy") {
		return CodeSmell{}, false
	}

	actionCount := 0
	for _, part := range nameParts {
		if actionVerbs[part] {
			actionCount++
		}
	}
	if actionCount == 0 {
		return CodeSmell{}, false
	}

	// Primary signal: redirect language in body
	// Strong phrases are high confidence regardless of edge count.
	// Weak phrases ("run `", "use `") are only flagged if the function has
	// ZERO outgoing edges — otherwise it does real work and the phrase is
	// just in a usage/error/help string.
	hasRedirectLanguage := false
	reason := ""

	strongPhrases := []string{
		"available in interactive", "use the cli",
		"not available", "instead use",
	}
	for _, phrase := range strongPhrases {
		if strings.Contains(lowerBody, phrase) {
			hasRedirectLanguage = true
			reason = "contains redirect language ('" + phrase + "')"
			break
		}
	}

	if !hasRedirectLanguage && c.OutEdges == 0 {
		weakPhrases := []string{"run `", "use `", "see `", "check `", "try running"}
		for _, phrase := range weakPhrases {
			if strings.Contains(lowerBody, phrase) {
				hasRedirectLanguage = true
				reason = "contains redirect language ('" + phrase + "') with 0 outgoing call edges"
				break
			}
		}
	}

	if !hasRedirectLanguage {
		return CodeSmell{}, false
	}

	// Secondary signal: name/edge divergence with shingle analysis
	nameScore := float64(actionCount) * float64(len(nameParts))
	edgePenalty := 1.0 / float64(c.OutEdges+1)

	shingleBoost := 0.0
	if c.OutEdges == 0 {
		sym := Symbol{Name: c.Name, Line: c.Line, File: c.File}
		// Detect language from file path so per-language stopwords apply.
		lang := DetectLanguage(c.File)
		shingles := ShinglesForSymbol(sym, sourceData, lang)
		if len(shingles) > 10 {
			shingleBoost = float64(len(shingles)) * 0.05
		}
	}

	score := nameScore * edgePenalty * (5.0 + shingleBoost)

	return CodeSmell{
		Kind:      SmellLazyRedirect,
		Name:      c.Name,
		File:      c.File,
		Line:      c.Line,
		FuncKind:  c.Kind,
		OutEdges:  c.OutEdges,
		InEdges:   c.InEdges,
		Score:     score,
		Reason:    reason,
		Signature: c.Signature,
	}, true
}

func detectStub(c FunctionEdgeInfo, bodyCalls int, bodyLines []string) (CodeSmell, bool) {
	// Skip code analysis files
	if isCodeAnalysisFile(c.File) {
		return CodeSmell{}, false
	}

	// Dunder methods (__init__, __lt__, …) are invoked implicitly by the
	// runtime; the AST cannot trace SomeClass(...) -> __init__, so they always
	// look edge-less. Never a stub. (#42)
	if strings.HasPrefix(c.Name, "__") && strings.HasSuffix(c.Name, "__") && len(c.Name) > 4 {
		return CodeSmell{}, false
	}

	// @abstractmethod-decorated methods are interface declarations, never stubs. (#43)
	for _, dec := range strings.Split(c.Decorators, ",") {
		if strings.TrimSpace(dec) == "abstractmethod" {
			return CodeSmell{}, false
		}
	}
	// Methods on Protocol/ABC classes have empty bodies by design. The
	// HasSuffix("Protocol") check intentionally also skips classes named *Protocol
	// (e.g. AuthManagerProtocol) — acceptable because Protocol classes are
	// definitionally interface-only, so silencing a rare concrete *Protocol class
	// is preferable to false-positive stub reports. (#43)
	for _, base := range strings.Split(c.BaseClasses, ",") {
		base = strings.TrimSpace(base)
		if base == "Protocol" || base == "ABC" || base == "ABCMeta" || strings.HasSuffix(base, "Protocol") {
			return CodeSmell{}, false
		}
	}

	// If body has calls but graph missed them, not a stub
	if bodyCalls > 0 {
		return CodeSmell{}, false
	}

	// Skip very short utility names (min, max, abs, etc.)
	if len(c.Name) <= 3 {
		return CodeSmell{}, false
	}

	// Check if the body has a return statement with a non-trivial value.
	// Functions that return struct literals, computed values, or formatted strings
	// are simple value functions, not stubs.
	meaningfulLines := 0
	hasReturn := false
	for _, line := range bodyLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "{" || trimmed == "}" ||
			strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		meaningfulLines++
		if strings.HasPrefix(trimmed, "return ") || strings.HasPrefix(trimmed, "return\t") {
			// Check if it returns something meaningful (not just nil/false/0)
			returnVal := strings.TrimPrefix(trimmed, "return ")
			returnVal = strings.TrimPrefix(returnVal, "return\t")
			returnVal = strings.TrimSpace(returnVal)
			if returnVal != "" && returnVal != "nil" && returnVal != "false" &&
				returnVal != "0" && returnVal != "\"\"" && returnVal != "None" &&
				returnVal != "null" && returnVal != "true" {
				hasReturn = true
			}
		}
	}

	// If function has a meaningful return value, it's a simple function, not a stub
	if hasReturn {
		return CodeSmell{}, false
	}

	// Score: zero-caller stubs are more likely dead code — unless the
	// method implements an interface, in which case it is called through
	// the interface (often by code outside the module, like fmt or sort)
	// and has no direct caller by design.
	score := 3.0
	reason := "zero outgoing calls and zero body calls"
	switch {
	case c.InEdges == 0 && c.Implements != "":
		reason = "zero outgoing calls; reached through " + c.Implements + ", not dead code"
	case c.InEdges == 0:
		score += 2.0
		reason = "zero outgoing AND incoming edges (likely dead code)"
	}
	if c.Resolution == GoResolutionApproximate {
		reason += " (approximate: file did not type-check, edges may be missing)"
	}

	return CodeSmell{
		Kind:      SmellStub,
		Name:      c.Name,
		File:      c.File,
		Line:      c.Line,
		FuncKind:  c.Kind,
		OutEdges:  c.OutEdges,
		InEdges:   c.InEdges,
		Score:     score,
		Reason:    reason,
		Signature: c.Signature,
	}, true
}

func detectPlaceholder(c FunctionEdgeInfo, body, lowerBody string, bodyLines []string) (CodeSmell, bool) {
	// Structural signal: zero outgoing edges + short body
	if c.OutEdges > 1 {
		return CodeSmell{}, false
	}

	// Must contain placeholder language
	placeholderPhrases := []string{
		"not yet implemented", "not implemented", "todo: implement",
		"fixme: implement", "unimplemented",
	}
	// Exclude "stub" and "placeholder" — too many false positives from code that
	// discusses these concepts (tool descriptions, detection logic, textarea config).
	found := ""
	for _, phrase := range placeholderPhrases {
		if strings.Contains(lowerBody, phrase) {
			// Verify the match is NOT inside a string literal that's part of a search
			// pattern, tool description, or code review pattern list
			found = phrase
			break
		}
	}
	if found == "" {
		return CodeSmell{}, false
	}

	// Skip code analysis tool files (contain pattern strings as data, not actual issues)
	if isCodeAnalysisFile(c.File) || strings.HasPrefix(c.Name, "detect") || strings.HasPrefix(c.Name, "NewCode") {
		return CodeSmell{}, false
	}

	// Score: short body + zero edges + placeholder language = high confidence
	meaningfulLines := 0
	for _, line := range bodyLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed != "{" && trimmed != "}" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") {
			meaningfulLines++
		}
	}

	score := 5.0
	if meaningfulLines <= 3 {
		score += 3.0 // very short body
	}
	if c.OutEdges == 0 {
		score += 2.0 // completely isolated
	}

	// Extract the snippet with the placeholder
	snippet := ""
	for _, line := range bodyLines {
		if strings.Contains(strings.ToLower(line), found) {
			snippet = strings.TrimSpace(line)
			break
		}
	}

	return CodeSmell{
		Kind:     SmellPlaceholder,
		Name:     c.Name,
		File:     c.File,
		Line:     c.Line,
		FuncKind: c.Kind,
		OutEdges: c.OutEdges,
		InEdges:  c.InEdges,
		Score:    score,
		Reason:   fmt.Sprintf("contains '%s' with only %d meaningful line(s) and %d outgoing edge(s)", found, meaningfulLines, c.OutEdges),
		Snippet:  snippet,
	}, true
}

func detectTodoFixme(c FunctionEdgeInfo, body string, bodyLines []string) []CodeSmell {
	if isCodeAnalysisFile(c.File) || strings.HasPrefix(c.Name, "detect") || strings.HasPrefix(c.Name, "NewCode") {
		return nil
	}

	markers := []struct {
		tag    string
		weight float64
	}{
		{"FIXME:", 4.0},
		{"HACK:", 3.5},
		{"XXX:", 3.0},
		{"TODO:", 2.0},
	}

	var results []CodeSmell
	for lineIdx, line := range bodyLines {
		for _, marker := range markers {
			if strings.Contains(line, marker.tag) {
				// Skip if the marker is inside a string literal (e.g., search patterns)
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "{\"") ||
					strings.Contains(trimmed, "Searches:") || strings.Contains(trimmed, "[]string{") {
					break
				}
				// Score boost: TODO in a highly-connected function is more critical
				score := marker.weight
				if c.InEdges > 5 {
					score += 2.0 // many callers = high impact
				}
				if c.OutEdges == 0 {
					score += 1.0 // might be blocking other work
				}

				snippet := strings.TrimSpace(line)
				if len(snippet) > 120 {
					snippet = snippet[:120] + "..."
				}

				results = append(results, CodeSmell{
					Kind:     SmellTodoFixme,
					Name:     c.Name,
					File:     c.File,
					Line:     c.Line + lineIdx,
					FuncKind: c.Kind,
					OutEdges: c.OutEdges,
					InEdges:  c.InEdges,
					Score:    score,
					Reason:   fmt.Sprintf("%s in %s (called by %d)", marker.tag, c.Name, c.InEdges),
					Snippet:  snippet,
				})
				break // one marker per line
			}
		}
	}
	return results
}

func detectEmptyHandler(c FunctionEdgeInfo, body, lowerBody string) (CodeSmell, bool) {
	if isCodeAnalysisFile(c.File) || strings.HasPrefix(c.Name, "detect") || strings.HasPrefix(c.Name, "NewCode") {
		return CodeSmell{}, false
	}

	// Look for error swallowing patterns
	swallowPatterns := []struct {
		pattern string
		reason  string
	}{
		{"_ = err", "assigns error to blank identifier"},
		{"_ , err", "discards value alongside error"},
		// ignore error in comment near an error variable
	}

	for _, sp := range swallowPatterns {
		if strings.Contains(body, sp.pattern) {
			// Count how many times errors are swallowed
			count := strings.Count(body, sp.pattern)

			// Score: more swallowed errors = worse
			score := float64(count) * 3.0
			if c.InEdges > 3 {
				score += 2.0 // high-impact function
			}

			// Structural signal: if the function has outgoing edges to error-returning
			// functions but no edges to logging/error-handling functions, that's worse
			if c.OutEdges > 0 {
				score += 1.0 // calls things that might return errors
			}

			return CodeSmell{
				Kind:     SmellEmptyHandler,
				Name:     c.Name,
				File:     c.File,
				Line:     c.Line,
				FuncKind: c.Kind,
				OutEdges: c.OutEdges,
				InEdges:  c.InEdges,
				Score:    score,
				Reason:   fmt.Sprintf("%s (%dx in %s)", sp.reason, count, c.Name),
			}, true
		}
	}

	// Also detect: `// ignore error` comments near error assignments
	if strings.Contains(lowerBody, "// ignore error") || strings.Contains(lowerBody, "// swallow") {
		return CodeSmell{
			Kind:     SmellEmptyHandler,
			Name:     c.Name,
			File:     c.File,
			Line:     c.Line,
			FuncKind: c.Kind,
			OutEdges: c.OutEdges,
			InEdges:  c.InEdges,
			Score:    2.0,
			Reason:   "explicit error suppression comment in " + c.Name,
		}, true
	}

	return CodeSmell{}, false
}

func detectHardcoded(c FunctionEdgeInfo, body string, bodyLines []string) []CodeSmell {
	if isCodeAnalysisFile(c.File) || strings.HasPrefix(c.Name, "detect") || strings.HasPrefix(c.Name, "NewCode") {
		return nil
	}

	type hardcodePattern struct {
		check  func(string) bool
		reason string
		weight float64
	}

	patterns := []hardcodePattern{
		{
			check:  func(line string) bool { return strings.Contains(line, "localhost") && strings.Contains(line, "://") },
			reason: "hardcoded localhost URL",
			weight: 3.0,
		},
		{
			check: func(line string) bool {
				return strings.Contains(line, "127.0.0.1") || strings.Contains(line, "0.0.0.0")
			},
			reason: "hardcoded IP address",
			weight: 3.0,
		},
		{
			check: func(line string) bool {
				// Only flag lines that assign a literal credential value, not field names/keys.
				// Pattern: variable = "sk-...", "password123", etc. (actual secret values)
				// Exclude: field names like `api_key`, config reads, struct field declarations
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
					return false
				}
				// Must have an assignment with a string literal value
				if !strings.Contains(line, "= \"") && !strings.Contains(line, "=\"") {
					return false
				}
				lower := strings.ToLower(line)
				// Check for actual hardcoded secret patterns (API key prefixes, passwords)
				return strings.Contains(lower, "sk-") || strings.Contains(lower, "pk-") ||
					strings.Contains(lower, "password123") || strings.Contains(lower, "changeme") ||
					strings.Contains(lower, "hunter2")
			},
			reason: "potential hardcoded credential value",
			weight: 5.0,
		},
	}

	var results []CodeSmell
	for lineIdx, line := range bodyLines {
		for _, p := range patterns {
			if p.check(line) {
				snippet := strings.TrimSpace(line)
				if len(snippet) > 120 {
					snippet = snippet[:120] + "..."
				}

				results = append(results, CodeSmell{
					Kind:     SmellHardcoded,
					Name:     c.Name,
					File:     c.File,
					Line:     c.Line + lineIdx,
					FuncKind: c.Kind,
					OutEdges: c.OutEdges,
					InEdges:  c.InEdges,
					Score:    p.weight,
					Reason:   p.reason + " in " + c.Name,
					Snippet:  snippet,
				})
				break // one finding per line
			}
		}
	}
	return results
}

// funcDefPattern matches the start of a function/method definition across languages.
var funcDefPattern = regexp.MustCompile(`(?m)^(?:\s*(?:func|def|function|fn|pub\s+fn|async\s+function|export\s+function|export\s+default\s+function)\s+\w)`)

// bodyCallPattern matches identifier followed by '(' — a call heuristic.
var bodyCallPattern = regexp.MustCompile(`\b([a-zA-Z_]\w*)\s*\(`)

// bodyCallKeywords are identifiers that look like calls but aren't.
var bodyCallKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"func": true, "function": true, "def": true, "fn": true,
	"return": true, "typeof": true, "instanceof": true, "sizeof": true,
	"make": true, "new": true, "delete": true, "type": true,
	"elif": true, "except": true, "with": true, "assert": true,
	"match": true, "case": true, "select": true, "go": true, "defer": true,
	"var": true, "let": true, "const": true, "range": true,
}

// findScopedBody extracts the body of a function, stopping at the next
// function definition rather than reading a fixed 50-line window.
// This prevents body bleed in Python/JS where functions aren't brace-delimited.
func findScopedBody(source []byte, sym Symbol) string {
	lines := strings.Split(string(source), "\n")
	if sym.Line <= 0 || sym.Line > len(lines) {
		return ""
	}

	start := sym.Line // skip the definition line itself (1-based → 0-indexed body start)
	if start >= len(lines) {
		return ""
	}

	maxEnd := start + 50
	if maxEnd > len(lines) {
		maxEnd = len(lines)
	}

	// Scan forward, stop at the next function definition or 50 lines
	end := maxEnd
	for i := start; i < maxEnd; i++ {
		if funcDefPattern.MatchString(lines[i]) {
			end = i
			break
		}
	}

	if end <= start {
		if start < len(lines) {
			return lines[start]
		}
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

// countBodyCalls counts the number of distinct call-like patterns (name() )
// in a function body, excluding keywords and the function's own name.
// This provides a source-level call count independent of graph edge resolution.
func countBodyCalls(body string, funcName string) int {
	matches := bodyCallPattern.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	count := 0
	for _, m := range matches {
		callee := m[1]
		if bodyCallKeywords[callee] || callee == funcName || seen[callee] {
			continue
		}
		seen[callee] = true
		count++
	}
	return count
}

// isCodeAnalysisFile returns true if the file is part of the code analysis
// tooling itself (code_review, code_smells, code_stubs, code_lazy_redirects).
// These files contain detection patterns as string data and should not be
// flagged as having those patterns.
func isCodeAnalysisFile(file string) bool {
	return strings.Contains(file, "code_review") ||
		strings.Contains(file, "code_smells") ||
		strings.Contains(file, "code_stubs") ||
		strings.Contains(file, "code_lazy") ||
		strings.Contains(file, "codegraph/")
}

// PackageGraph returns package-level connectivity for visualization.
func (idx *Indexer) PackageGraph() ([]PackageInfo, []PackageEdge, error) {
	return idx.store.GetPackageGraph()
}

// fileContentHash computes a SHA-256 hash of a file's content.
func fileContentHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]), nil // first 8 bytes is enough
}
