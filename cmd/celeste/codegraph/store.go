package codegraph

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SymbolKind identifies the kind of code symbol.
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolMethod    SymbolKind = "method"
	SymbolType      SymbolKind = "type"
	SymbolInterface SymbolKind = "interface"
	SymbolConst     SymbolKind = "const"
	SymbolVar       SymbolKind = "var"
	SymbolStruct    SymbolKind = "struct"
	SymbolImport    SymbolKind = "import"
	SymbolClass     SymbolKind = "class"
)

// EdgeKind identifies the kind of relationship between symbols.
type EdgeKind string

const (
	EdgeCalls      EdgeKind = "calls"
	EdgeImports    EdgeKind = "imports"
	EdgeImplements EdgeKind = "implements"
	EdgeEmbeds     EdgeKind = "embeds"
	EdgeReferences EdgeKind = "references"
)

// Symbol represents a code entity (function, type, interface, etc.).
type Symbol struct {
	ID        int64
	Name      string
	Kind      SymbolKind
	Package   string
	File      string
	Line      int
	Signature string
}

// Edge represents a relationship between two symbols.
type Edge struct {
	SourceID int64
	TargetID int64
	Kind     EdgeKind
}

// FileRecord tracks indexed files for incremental updates.
type FileRecord struct {
	Path        string
	Language    string
	Size        int64
	ContentHash string
	IndexedAt   int64
}

// MinHashSignature is a fixed-length array of hash values for similarity search.
type MinHashSignature []uint64

// StoreStats holds aggregate counts for the indexed codebase.
type StoreStats struct {
	TotalSymbols  int
	TotalEdges    int
	TotalFiles    int
	SymbolsByKind map[SymbolKind]int
	FilesByLang   map[string]int
}

// MinHashEntry pairs a symbol ID with its MinHash signature for bulk queries.
type MinHashEntry struct {
	SymbolID  int64
	Signature MinHashSignature
}

// Store manages the SQLite database for the code graph.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at the given path and
// initializes the schema.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// Enable foreign keys.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS symbols (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		package TEXT,
		file TEXT NOT NULL,
		line INTEGER,
		signature TEXT,
		minhash BLOB
	);

	CREATE TABLE IF NOT EXISTS edges (
		source_id INTEGER REFERENCES symbols(id) ON DELETE CASCADE,
		target_id INTEGER REFERENCES symbols(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		UNIQUE(source_id, target_id, kind)
	);

	CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		language TEXT,
		size INTEGER,
		content_hash TEXT,
		indexed_at INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
	CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file);
	CREATE INDEX IF NOT EXISTS idx_symbols_package ON symbols(package);
	CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
	CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// UpsertSymbol inserts or updates a symbol. Uniqueness is determined by
// (name, kind, package, file). Returns the row ID.
func (s *Store) UpsertSymbol(sym Symbol) (int64, error) {
	// Check if symbol already exists.
	var existingID int64
	err := s.db.QueryRow(
		`SELECT id FROM symbols WHERE name = ? AND kind = ? AND package = ? AND file = ?`,
		sym.Name, sym.Kind, sym.Package, sym.File,
	).Scan(&existingID)

	if err == nil {
		// Update existing row.
		_, err = s.db.Exec(
			`UPDATE symbols SET line = ?, signature = ? WHERE id = ?`,
			sym.Line, sym.Signature, existingID,
		)
		return existingID, err
	}

	// Insert new row.
	result, err := s.db.Exec(
		`INSERT INTO symbols (name, kind, package, file, line, signature)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sym.Name, sym.Kind, sym.Package, sym.File, sym.Line, sym.Signature,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetSymbol retrieves a symbol by its ID.
func (s *Store) GetSymbol(id int64) (*Symbol, error) {
	sym := &Symbol{}
	err := s.db.QueryRow(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, '')
		 FROM symbols WHERE id = ?`, id,
	).Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Package, &sym.File, &sym.Line, &sym.Signature)
	if err != nil {
		return nil, err
	}
	return sym, nil
}

// AddEdge records a directional relationship between two symbols.
func (s *Store) AddEdge(sourceID, targetID int64, kind EdgeKind) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO edges (source_id, target_id, kind) VALUES (?, ?, ?)`,
		sourceID, targetID, kind,
	)
	return err
}

// GetEdgesFrom returns all outgoing edges from the given symbol.
func (s *Store) GetEdgesFrom(sourceID int64) ([]Edge, error) {
	rows, err := s.db.Query(
		`SELECT source_id, target_id, kind FROM edges WHERE source_id = ?`, sourceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetEdgesTo returns all incoming edges to the given symbol.
func (s *Store) GetEdgesTo(targetID int64) ([]Edge, error) {
	rows, err := s.db.Query(
		`SELECT source_id, target_id, kind FROM edges WHERE target_id = ?`, targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func scanEdges(rows *sql.Rows) ([]Edge, error) {
	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.SourceID, &e.TargetID, &e.Kind); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// UpsertFile inserts or updates a file record.
func (s *Store) UpsertFile(f FileRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO files (path, language, size, content_hash, indexed_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   language = excluded.language,
		   size = excluded.size,
		   content_hash = excluded.content_hash,
		   indexed_at = excluded.indexed_at`,
		f.Path, f.Language, f.Size, f.ContentHash, time.Now().Unix(),
	)
	return err
}

// GetFile retrieves a file record by path.
func (s *Store) GetFile(path string) (*FileRecord, error) {
	f := &FileRecord{}
	err := s.db.QueryRow(
		`SELECT path, language, size, content_hash, indexed_at FROM files WHERE path = ?`, path,
	).Scan(&f.Path, &f.Language, &f.Size, &f.ContentHash, &f.IndexedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteFile removes a file record.
func (s *Store) DeleteFile(path string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE path = ?`, path)
	return err
}

// DeleteFileSymbols removes all symbols (and their edges) for a file.
func (s *Store) DeleteFileSymbols(file string) error {
	// First delete edges that reference symbols in this file.
	_, err := s.db.Exec(
		`DELETE FROM edges WHERE source_id IN (SELECT id FROM symbols WHERE file = ?)
		 OR target_id IN (SELECT id FROM symbols WHERE file = ?)`, file, file,
	)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM symbols WHERE file = ?`, file)
	return err
}

// GetSymbolsByFile returns all symbols in the given file.
func (s *Store) GetSymbolsByFile(file string) ([]Symbol, error) {
	rows, err := s.db.Query(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, '')
		 FROM symbols WHERE file = ? ORDER BY line`, file,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetSymbolsByPackage returns all symbols in the given package.
func (s *Store) GetSymbolsByPackage(pkg string) ([]Symbol, error) {
	rows, err := s.db.Query(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, '')
		 FROM symbols WHERE package = ? ORDER BY file, line`, pkg,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// SearchSymbolsByName returns symbols whose name contains the query (case-insensitive).
func (s *Store) SearchSymbolsByName(query string) ([]Symbol, error) {
	rows, err := s.db.Query(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, '')
		 FROM symbols WHERE name LIKE ? ORDER BY name`,
		"%"+query+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	var syms []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Package, &sym.File, &sym.Line, &sym.Signature); err != nil {
			return nil, err
		}
		syms = append(syms, sym)
	}
	return syms, rows.Err()
}

// GetSymbolIDByName returns the ID of a symbol by exact name match.
// If multiple symbols share the same name, returns the first found.
func (s *Store) GetSymbolIDByName(name string) (int64, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM symbols WHERE name = ? LIMIT 1`, name).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// UpdateMinHash stores the MinHash signature for a symbol.
func (s *Store) UpdateMinHash(symbolID int64, sig MinHashSignature) error {
	blob := encodeMinHash(sig)
	_, err := s.db.Exec(`UPDATE symbols SET minhash = ? WHERE id = ?`, blob, symbolID)
	return err
}

// GetMinHash retrieves the MinHash signature for a symbol.
func (s *Store) GetMinHash(symbolID int64) (MinHashSignature, error) {
	var blob []byte
	err := s.db.QueryRow(`SELECT minhash FROM symbols WHERE id = ?`, symbolID).Scan(&blob)
	if err != nil {
		return nil, err
	}
	return decodeMinHash(blob), nil
}

// GetAllMinHashes retrieves all symbol IDs and their MinHash signatures
// for similarity search. Symbols without a signature are skipped.
func (s *Store) GetAllMinHashes() ([]MinHashEntry, error) {
	rows, err := s.db.Query(`SELECT id, minhash FROM symbols WHERE minhash IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MinHashEntry
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		entries = append(entries, MinHashEntry{
			SymbolID:  id,
			Signature: decodeMinHash(blob),
		})
	}
	return entries, rows.Err()
}

// PackageEdge represents a connection between two packages.
type PackageEdge struct {
	Source string
	Target string
	Count  int
}

// PackageInfo holds package-level stats for visualization.
type PackageInfo struct {
	Name        string
	SymbolCount int
	FileCount   int
}

// FileEdge represents a connection between two files.
type FileEdge struct {
	Source string
	Target string
	Count  int
}

// GetFileGraph returns file-level connectivity data for visualization.
// Works for all languages — shows which files call into other files.
func (s *Store) GetFileGraph() ([]FileEdge, error) {
	rows, err := s.db.Query(`
		SELECT src.file, dst.file, COUNT(*) as edge_count
		FROM edges e
		JOIN symbols src ON e.source_id = src.id
		JOIN symbols dst ON e.target_id = dst.id
		WHERE src.file != dst.file
		GROUP BY src.file, dst.file
		ORDER BY edge_count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []FileEdge
	for rows.Next() {
		var e FileEdge
		if err := rows.Scan(&e.Source, &e.Target, &e.Count); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// GetPackageGraph returns package-level connectivity data for visualization.
func (s *Store) GetPackageGraph() ([]PackageInfo, []PackageEdge, error) {
	// Get package info
	rows, err := s.db.Query(`
		SELECT package, COUNT(*) as sym_count, COUNT(DISTINCT file) as file_count
		FROM symbols WHERE package != '' GROUP BY package ORDER BY sym_count DESC
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var packages []PackageInfo
	for rows.Next() {
		var p PackageInfo
		if err := rows.Scan(&p.Name, &p.SymbolCount, &p.FileCount); err != nil {
			return nil, nil, err
		}
		packages = append(packages, p)
	}

	// Get package-level edges
	rows2, err := s.db.Query(`
		SELECT src.package as src_pkg, dst.package as dst_pkg, COUNT(*) as edge_count
		FROM edges e
		JOIN symbols src ON e.source_id = src.id
		JOIN symbols dst ON e.target_id = dst.id
		WHERE src.package != '' AND dst.package != '' AND src.package != dst.package
		GROUP BY src_pkg, dst_pkg
		ORDER BY edge_count DESC
	`)
	if err != nil {
		return packages, nil, err
	}
	defer rows2.Close()

	var edges []PackageEdge
	for rows2.Next() {
		var e PackageEdge
		if err := rows2.Scan(&e.Source, &e.Target, &e.Count); err != nil {
			return packages, nil, err
		}
		edges = append(edges, e)
	}

	return packages, edges, rows2.Err()
}

// Stats returns aggregate counts for the indexed codebase.
func (s *Store) Stats() (*StoreStats, error) {
	stats := &StoreStats{
		SymbolsByKind: make(map[SymbolKind]int),
		FilesByLang:   make(map[string]int),
	}

	// Total symbols
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&stats.TotalSymbols)

	// Total edges
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&stats.TotalEdges)

	// Total files
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&stats.TotalFiles)

	// Symbols by kind
	rows, err := s.db.Query(`SELECT kind, COUNT(*) FROM symbols GROUP BY kind`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var kind SymbolKind
			var count int
			_ = rows.Scan(&kind, &count)
			stats.SymbolsByKind[kind] = count
		}
	}

	// Files by language
	rows2, err := s.db.Query(`SELECT language, COUNT(*) FROM files GROUP BY language`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var lang string
			var count int
			_ = rows2.Scan(&lang, &count)
			stats.FilesByLang[lang] = count
		}
	}

	return stats, nil
}

// GetAllFiles returns all indexed file records.
func (s *Store) GetAllFiles() ([]FileRecord, error) {
	rows, err := s.db.Query(`SELECT path, language, size, content_hash, indexed_at FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.Path, &f.Language, &f.Size, &f.ContentHash, &f.IndexedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// StubResult represents a function/method with zero outgoing call edges.
type StubResult struct {
	Name     string
	File     string
	Line     int
	Kind     string
	OutEdges int
	InEdges  int
}

// FindStubs returns functions/methods with zero outgoing call edges.
// These are likely stubs, placeholders, or dead code.
func (s *Store) FindStubs(includeTests bool) ([]StubResult, error) {
	query := `
		SELECT s.name, s.file, s.line, s.kind,
		       (SELECT COUNT(*) FROM edges e WHERE e.source_id = s.id) as calls_out,
		       (SELECT COUNT(*) FROM edges e WHERE e.target_id = s.id) as called_by
		FROM symbols s
		WHERE s.kind IN ('function', 'method')
		AND (SELECT COUNT(*) FROM edges e WHERE e.source_id = s.id) = 0
		ORDER BY called_by ASC, s.file, s.line
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("find stubs query: %w", err)
	}
	defer rows.Close()

	var results []StubResult
	for rows.Next() {
		var r StubResult
		if err := rows.Scan(&r.Name, &r.File, &r.Line, &r.Kind, &r.OutEdges, &r.InEdges); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// LazyRedirectCandidate represents a function whose name implies complex behavior
// but whose graph structure shows it's structurally trivial — a potential lazy redirect.
type LazyRedirectCandidate struct {
	Name      string
	File      string
	Line      int
	Kind      string
	OutEdges  int
	InEdges   int
	Signature string
}

// FindLazyRedirectCandidates returns functions/methods with low outgoing edges
// (0-2) that are NOT known leaf patterns (constructors, getters, interface impls).
// These are candidates for lazy redirect analysis via shingle/edge divergence.
func (s *Store) FindLazyRedirectCandidates(includeTests bool) ([]LazyRedirectCandidate, error) {
	query := `
		SELECT s.id, s.name, s.file, s.line, s.kind, COALESCE(s.signature, ''),
		       (SELECT COUNT(*) FROM edges e WHERE e.source_id = s.id) as calls_out,
		       (SELECT COUNT(*) FROM edges e WHERE e.target_id = s.id) as called_by
		FROM symbols s
		WHERE s.kind IN ('function', 'method')
		AND (SELECT COUNT(*) FROM edges e WHERE e.source_id = s.id) <= 2
		ORDER BY calls_out ASC, s.file, s.line
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("find lazy redirect candidates: %w", err)
	}
	defer rows.Close()

	var results []LazyRedirectCandidate
	for rows.Next() {
		var id int64
		var r LazyRedirectCandidate
		if err := rows.Scan(&id, &r.Name, &r.File, &r.Line, &r.Kind, &r.Signature, &r.OutEdges, &r.InEdges); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindAllFunctionsWithEdges returns all functions/methods with their edge counts.
// Used by the unified code smell detector for single-pass analysis.
func (s *Store) FindAllFunctionsWithEdges() ([]FunctionEdgeInfo, error) {
	query := `
		SELECT s.name, s.file, s.line, s.kind, COALESCE(s.signature, ''),
		       (SELECT COUNT(*) FROM edges e WHERE e.source_id = s.id) as calls_out,
		       (SELECT COUNT(*) FROM edges e WHERE e.target_id = s.id) as called_by
		FROM symbols s
		WHERE s.kind IN ('function', 'method')
		ORDER BY s.file, s.line
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("find all functions: %w", err)
	}
	defer rows.Close()

	var results []FunctionEdgeInfo
	for rows.Next() {
		var r FunctionEdgeInfo
		if err := rows.Scan(&r.Name, &r.File, &r.Line, &r.Kind, &r.Signature, &r.OutEdges, &r.InEdges); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// encodeMinHash converts a MinHash signature to a byte slice for BLOB storage.
func encodeMinHash(sig MinHashSignature) []byte {
	buf := make([]byte, len(sig)*8)
	for i, v := range sig {
		binary.LittleEndian.PutUint64(buf[i*8:], v)
	}
	return buf
}

// decodeMinHash converts a BLOB byte slice back to a MinHash signature.
func decodeMinHash(blob []byte) MinHashSignature {
	n := len(blob) / 8
	sig := make(MinHashSignature, n)
	for i := 0; i < n; i++ {
		sig[i] = binary.LittleEndian.Uint64(blob[i*8:])
	}
	return sig
}
