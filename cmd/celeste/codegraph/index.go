package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
}

// DefaultIndexPath returns the path to the code graph database for a project.
// It stores the index under ~/.celeste/projects/<hash>/codegraph.db to avoid
// polluting the project directory.
func DefaultIndexPath(projectRoot string) string {
	homeDir, _ := os.UserHomeDir()
	hash := sha256.Sum256([]byte(projectRoot))
	hexHash := hex.EncodeToString(hash[:8]) // first 8 bytes = 16 hex chars
	dir := filepath.Join(homeDir, ".celeste", "projects", hexHash)
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "codegraph.db")
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
	return idx.store.Close()
}

// Store returns the underlying store for direct queries (used by tools).
func (idx *Indexer) Store() *Store {
	return idx.store
}

// Build performs a full index of the workspace. Walks the file tree,
// parses source files, extracts symbols and edges, computes MinHash
// signatures, and stores everything in SQLite.
func (idx *Indexer) Build() error {
	files, err := idx.walkSourceFiles()
	if err != nil {
		return fmt.Errorf("walk files: %w", err)
	}

	for _, path := range files {
		if err := idx.indexFile(path); err != nil {
			// Log but don't fail on individual file errors
			continue
		}
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

	return nil
}

// Update performs an incremental update. Only re-indexes files whose
// content hash has changed since the last index. Removes symbols for
// deleted files.
func (idx *Indexer) Update() error {
	// Get currently indexed files
	indexedFiles, err := idx.store.GetAllFiles()
	if err != nil {
		return fmt.Errorf("get indexed files: %w", err)
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
	for path := range indexedMap {
		if !currentSet[path] {
			_ = idx.store.DeleteFileSymbols(path)
			_ = idx.store.DeleteFile(path)
		}
	}

	// Index new or changed files
	for _, path := range currentFiles {
		hash, err := fileContentHash(filepath.Join(idx.workspace, path))
		if err != nil {
			continue
		}
		if existing, ok := indexedMap[path]; ok && existing.ContentHash == hash {
			continue // unchanged
		}
		// Re-index this file
		_ = idx.store.DeleteFileSymbols(path)
		if err := idx.indexFile(path); err != nil {
			continue
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

	return nil
}

// indexFile parses a single file and stores its symbols, edges, and MinHash.
func (idx *Indexer) indexFile(relPath string) error {
	absPath := filepath.Join(idx.workspace, relPath)
	lang := DetectLanguage(relPath)

	var result *ParseResult
	var err error

	if lang == "go" {
		parser := NewGoParser()
		result, err = parser.ParseFile(absPath)
	} else if lang == "typescript" {
		// Tree-sitter backed TS parser. Lazily allocated on first use
		// so pure-Go / Python projects pay no CGo startup cost.
		if idx.tsParser == nil {
			idx.tsParser = NewTSParser()
		}
		result, err = idx.tsParser.ParseFile(absPath)
	} else if indexableLanguages[lang] {
		parser := NewGenericParser(lang)
		result, err = parser.ParseFile(absPath)
	} else {
		return nil // no parser for this language
	}

	if err != nil {
		return err
	}

	// Read source for shingle generation
	source, _ := os.ReadFile(absPath)

	// Store symbols and compute MinHash
	symbolIDs := make(map[string]int64) // name -> ID
	for _, sym := range result.Symbols {
		sym.File = relPath // use relative path
		id, err := idx.store.UpsertSymbol(sym)
		if err != nil {
			continue
		}
		symbolIDs[sym.Name] = id

		// Compute MinHash for non-import symbols
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
		targetID, ok2 := symbolIDs[edge.TargetName]
		if !ok2 {
			targetID, ok2 = idx.store.GetSymbolIDByName(edge.TargetName)
		}
		// Try unqualified name: "pkg.Func" -> "Func"
		if !ok2 {
			if dotIdx := strings.LastIndex(edge.TargetName, "."); dotIdx >= 0 {
				unqualified := edge.TargetName[dotIdx+1:]
				targetID, ok2 = symbolIDs[unqualified]
				if !ok2 {
					targetID, ok2 = idx.store.GetSymbolIDByName(unqualified)
				}
			}
		}
		if ok1 && ok2 {
			_ = idx.store.AddEdge(sourceID, targetID, edge.Kind)
		}
	}

	// Store file record
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
	})

	return nil
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
			for _, id := range candidateIDs {
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
		for _, entry := range entries {
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

	return b.String()
}

// LazyRedirectResult is a scored candidate for lazy redirect detection.
type LazyRedirectResult struct {
	Name      string  `json:"name"`
	File      string  `json:"file"`
	Line      int     `json:"line"`
	Kind      string  `json:"kind"`
	OutEdges  int     `json:"outgoing_edges"`
	InEdges   int     `json:"incoming_edges"`
	Score     float64 `json:"divergence_score"`
	Reason    string  `json:"reason"`
	Signature string  `json:"signature"`
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

// FindLazyRedirects uses structural analysis to detect functions whose names
// imply complex behavior but whose graph structure shows they're trivially simple.
// This goes beyond grep-based detection by measuring the divergence between a
// function's semantic vocabulary (shingles) and its actual call graph connectivity.
//
// Scoring factors:
//   - Name complexity: action verbs in name suggest the function should DO work
//   - Edge poverty: fewer outgoing edges = less actual work done
//   - Shingle richness: domain-specific vocabulary in body that doesn't connect to edges
//
// Returns results sorted by divergence score (highest = most suspicious).
func (idx *Indexer) FindLazyRedirects(maxResults int, includeTests bool) ([]LazyRedirectResult, error) {
	candidates, err := idx.store.FindLazyRedirectCandidates(includeTests)
	if err != nil {
		return nil, err
	}

	var results []LazyRedirectResult

	for _, c := range candidates {
		if !includeTests && isTestFilePath(c.File) {
			continue
		}
		if isExpectedLeaf(c.Name) {
			continue
		}

		absFile := c.File
		if !filepath.IsAbs(absFile) {
			absFile = filepath.Join(idx.workspace, absFile)
		}
		sourceData, _ := os.ReadFile(absFile)
		sym := Symbol{Name: c.Name, Line: c.Line}
		body := ""
		lowerBody := ""
		if sourceData != nil {
			body = findSymbolBody(sourceData, sym)
			lowerBody = strings.ToLower(body)
		}

		info := FunctionEdgeInfo{
			Name: c.Name, File: c.File, Line: c.Line,
			Kind: c.Kind, Signature: c.Signature,
			OutEdges: c.OutEdges, InEdges: c.InEdges,
		}
		smell, ok := detectLazyRedirect(info, body, lowerBody, sourceData)
		if !ok {
			continue
		}

		results = append(results, LazyRedirectResult{
			Name:      smell.Name,
			File:      smell.File,
			Line:      smell.Line,
			Kind:      smell.FuncKind,
			OutEdges:  smell.OutEdges,
			InEdges:   smell.InEdges,
			Score:     smell.Score,
			Reason:    smell.Reason,
			Signature: smell.Signature,
		})
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

// isTestFilePath returns true if the file path looks like a test file.
func isTestFilePath(file string) bool {
	return strings.HasSuffix(file, "_test.go") ||
		strings.HasSuffix(file, "_test.py") ||
		strings.HasSuffix(file, ".test.ts") ||
		strings.HasSuffix(file, ".test.js") ||
		strings.HasSuffix(file, ".spec.ts") ||
		strings.HasSuffix(file, ".spec.js") ||
		strings.Contains(file, "/test/") ||
		strings.Contains(file, "/tests/")
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
	Name      string
	File      string
	Line      int
	Kind      string
	Signature string
	OutEdges  int
	InEdges   int
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

	// Score: zero-caller stubs are more likely dead code
	score := 3.0
	reason := "zero outgoing calls and zero body calls"
	if c.InEdges == 0 {
		score += 2.0
		reason = "zero outgoing AND incoming edges (likely dead code)"
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
