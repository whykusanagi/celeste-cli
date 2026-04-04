package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SearchResult pairs a symbol with its similarity score.
type SearchResult struct {
	Symbol     Symbol
	Similarity float64
}

// Indexer manages the code graph lifecycle: build, update, and query.
type Indexer struct {
	workspace string
	store     *Store
	hasher    *MinHasher
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
func NewIndexer(workspace, dbPath string) (*Indexer, error) {
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	return &Indexer{
		workspace: workspace,
		store:     store,
		hasher:    NewMinHasher(DefaultNumHashes),
	}, nil
}

// NewIndexerWithStore creates an indexer using an existing store.
// This is useful for testing where the store is set up manually.
func NewIndexerWithStore(store *Store, workspace string) *Indexer {
	return &Indexer{
		workspace: workspace,
		store:     store,
		hasher:    NewMinHasher(DefaultNumHashes),
	}
}

// Close releases the underlying database connection.
func (idx *Indexer) Close() error {
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
			shingles := ShinglesForSymbol(sym, source)
			sig := idx.hasher.Signature(shingles)
			_ = idx.store.UpdateMinHash(id, sig)
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

// SemanticSearch finds symbols semantically similar to the query string.
// The query is split into shingles, MinHashed, then compared against all
// symbol signatures using brute-force Jaccard similarity.
func (idx *Indexer) SemanticSearch(query string, topK int) ([]SearchResult, error) {
	// Generate query shingles from the query string
	words := strings.Fields(strings.ToLower(query))
	var queryShingles []string
	for _, w := range words {
		queryShingles = append(queryShingles, splitIdentifier(w)...)
	}
	queryShingles = deduplicateLowercase(queryShingles)

	querySig := idx.hasher.Signature(queryShingles)

	// Get all MinHash signatures
	entries, err := idx.store.GetAllMinHashes()
	if err != nil {
		return nil, fmt.Errorf("get minhashes: %w", err)
	}

	// Compute similarity for each symbol
	type scored struct {
		symbolID   int64
		similarity float64
	}
	var results []scored
	for _, entry := range entries {
		sim := JaccardSimilarity(querySig, entry.Signature)
		if sim > 0.05 { // minimum threshold
			results = append(results, scored{entry.SymbolID, sim})
		}
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	// Take top K
	if len(results) > topK {
		results = results[:topK]
	}

	// Resolve symbol details
	var searchResults []SearchResult
	for _, r := range results {
		sym, err := idx.store.GetSymbol(r.symbolID)
		if err != nil {
			continue
		}
		searchResults = append(searchResults, SearchResult{
			Symbol:     *sym,
			Similarity: r.similarity,
		})
	}

	return searchResults, nil
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

// fileContentHash computes a SHA-256 hash of a file's content.
func fileContentHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]), nil // first 8 bytes is enough
}
