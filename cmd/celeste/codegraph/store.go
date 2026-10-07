package codegraph

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
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
	// SymbolInterfaceMethod is a method declared inside a Go interface type.
	// It has no body; its outgoing "implements" edges lead to the concrete
	// methods a call through the interface can reach.
	SymbolInterfaceMethod SymbolKind = "interface_method"
)

// EdgeKind identifies the kind of relationship between symbols.
type EdgeKind string

// Edge kinds (docs/CODEGRAPH.md). Every parser emits EdgeCalls; the
// type-checked Go pass also emits the other two.
const (
	// EdgeCalls: the source calls the target, directly or through a
	// function value, struct field, map/slice element or interface method.
	EdgeCalls EdgeKind = "calls"
	// EdgeReferences: the source takes the target as a value (passes it,
	// stores it, returns it) without calling it at that point.
	EdgeReferences EdgeKind = "references"
	// EdgeImplements: the source is an interface method and the target is
	// a concrete method that implements it, so a call through the
	// interface can reach the target.
	EdgeImplements EdgeKind = "implements"
)

// Go call-graph resolution recorded per file in files.resolution.
const (
	// GoResolutionTyped: the file's package type-checked cleanly and every
	// call edge from it points at the exact function or method.
	GoResolutionTyped = "typed"
	// GoResolutionApproximate: the file did not type-check (broken code,
	// missing dependencies, build tags excluding it). Edges the type
	// checker could not resolve fall back to bare-name matching.
	GoResolutionApproximate = "approximate"
)

// Symbol represents a code entity (function, type, interface, etc.).
type Symbol struct {
	ID          int64
	Name        string
	Kind        SymbolKind
	Package     string
	File        string
	Line        int
	Signature   string
	Decorators  string // comma-separated decorator names captured at parse time
	BaseClasses string // comma-separated base-class names of the enclosing class
	// QualName is the type-checked qualified name of a Go function,
	// method, interface method, type, var or const, as go/types prints it:
	// "pkg/path.Func", "(*pkg/path.T).Method", "(pkg/path.I).Method".
	// Empty for other languages and for Go files that did not type-check.
	QualName string
	// Implements lists the interfaces a Go method satisfies (comma-separated,
	// e.g. "error,fmt.Stringer"). A method listed here is reached through
	// the interface even when no edge points at it.
	Implements string
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
	// Resolution is GoResolutionTyped or GoResolutionApproximate for Go
	// files and empty for every other language.
	Resolution string
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
	// path is the database file; the index lock file sits next to it.
	path string
}

// NewStore opens (or creates) a SQLite database at the given path and
// initializes the schema.
func NewStore(dbPath string) (*Store, error) {
	// synchronous=NORMAL on every pooled connection (a DSN pragma, unlike
	// db.Exec, reaches each one): in WAL mode a commit then appends to the
	// WAL without an fsync, which is deferred to checkpoints. Index writes
	// are autocommitted, about a hundred per symbol, and with the default
	// FULL each fsync costs tens of milliseconds on Windows, which made an
	// index build take seconds to minutes there (#385). The index is
	// derived data: a power loss can drop the last commits, never corrupt
	// the database. Commits are lost newest first and a file's record is
	// stored after its symbols, so a file whose symbols were dropped has
	// no record or an old content hash, and the next update re-indexes it.
	// An update does not repair an index whose file records survive
	// without their symbols (a symbol write that failed, say): it skips a
	// file whose content hash matches its record. `celeste index rebuild`
	// does.
	//
	// foreign_keys is a per-connection setting too, so it goes in the DSN
	// for the same reason: run once through db.Exec it reached only one
	// pooled connection, and ON DELETE CASCADE (symbol_tokens, lsh_bands,
	// edges) applied only to deletes that happened to run there (#389).
	//
	// busy_timeout comes first so every later pragma and statement waits
	// up to 10 s for another connection's or process's write lock instead
	// of failing at once with SQLITE_BUSY (#392).
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db, path: dbPath}
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
		decorators TEXT,
		base_classes TEXT,
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

	-- Opaque key/value store for index-level metadata that doesn't fit
	-- the other tables. Current uses:
	--   "minhash_seeds"        — little-endian uint64×128, the seeds used
	--                            by MinHasher. Persisted on first Build so
	--                            the same seeds can be restored on re-open,
	--                            making MinHash signatures portable across
	--                            process invocations. Without this, opening
	--                            an existing index with a fresh process
	--                            generated random seeds and SemanticSearch
	--                            returned pure noise.
	--   "minhash_num_hashes"   — uint64-as-string, sanity check on the
	--                            MinHasher length.
	--   "shingle_version"      — future: tokenizer version stamp for the
	--                            stale-index warning in a later change.
	CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value BLOB NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
	CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file);
	CREATE INDEX IF NOT EXISTS idx_symbols_package ON symbols(package);
	CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
	CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	for _, col := range []string{
		"ALTER TABLE symbols ADD COLUMN qual_name TEXT",
		"ALTER TABLE symbols ADD COLUMN implements TEXT",
		"ALTER TABLE files ADD COLUMN resolution TEXT",
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate schema: %w", err)
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_symbols_qual ON symbols(qual_name)`); err != nil {
		return err
	}
	// Idempotent migration: add decorators/base_classes columns to existing DBs
	// that were created before this schema version. The columns exist in new DBs
	// from the CREATE TABLE above; for existing DBs ALTER TABLE adds them.
	for _, col := range []string{
		"ALTER TABLE symbols ADD COLUMN decorators TEXT",
		"ALTER TABLE symbols ADD COLUMN base_classes TEXT",
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate symbols: %w", err)
		}
	}
	// BM25 tables (token_stats, symbol_tokens). Defined in bm25.go so
	// all BM25-related code lives together. Idempotent CREATE IF NOT
	// EXISTS so calling on every Open is safe; existing indexes get
	// the tables lazily without a migration step.
	if err := s.createBM25Schema(); err != nil {
		return err
	}
	// LSH band table for sub-linear semantic search. Defined in lsh.go.
	return s.createLSHSchema()
}

// GetMeta reads a raw byte value from the meta key/value table.
// Returns (nil, nil) if the key is not present — callers should treat
// nil as "not set" and decide whether to generate and persist.
func (s *Store) GetMeta(key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get meta %q: %w", key, err)
	}
	return value, nil
}

// SetMeta writes a raw byte value to the meta key/value table. Upserts
// on conflict so the caller can treat this as idempotent.
func (s *Store) SetMeta(key string, value []byte) error {
	_, err := s.db.Exec(
		"INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

// DeleteMeta removes a key from the meta table. Removing a missing key is
// not an error.
func (s *Store) DeleteMeta(key string) error {
	if _, err := s.db.Exec("DELETE FROM meta WHERE key = ?", key); err != nil {
		return fmt.Errorf("delete meta %q: %w", key, err)
	}
	return nil
}

// DeleteMetaIf removes a key only while it still holds value, so an
// indexer clears only the marks it set itself (#392).
func (s *Store) DeleteMetaIf(key, value string) error {
	if _, err := s.db.Exec("DELETE FROM meta WHERE key = ? AND value = ?", key, []byte(value)); err != nil {
		return fmt.Errorf("delete meta %q: %w", key, err)
	}
	return nil
}

// UpsertSymbol inserts or updates a symbol. Uniqueness is determined by
// (name, kind, package, file, qual_name), so two Go methods with the same
// name on different receivers in one file stay separate rows. Returns the
// row ID.
func (s *Store) UpsertSymbol(sym Symbol) (int64, error) {
	// Check if symbol already exists.
	var existingID int64
	err := s.db.QueryRow(
		`SELECT id FROM symbols WHERE name = ? AND kind = ? AND package = ? AND file = ?
		 AND COALESCE(qual_name, '') = ?`,
		sym.Name, sym.Kind, sym.Package, sym.File, sym.QualName,
	).Scan(&existingID)

	if err == nil {
		// Update existing row.
		_, err = s.db.Exec(
			`UPDATE symbols SET line = ?, signature = ?, decorators = ?, base_classes = ?, implements = ? WHERE id = ?`,
			sym.Line, sym.Signature, sym.Decorators, sym.BaseClasses, sym.Implements, existingID,
		)
		return existingID, err
	}

	// Insert new row.
	result, err := s.db.Exec(
		`INSERT INTO symbols (name, kind, package, file, line, signature, decorators, base_classes, qual_name, implements)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sym.Name, sym.Kind, sym.Package, sym.File, sym.Line, sym.Signature, sym.Decorators, sym.BaseClasses,
		sym.QualName, sym.Implements,
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
		`SELECT id, name, kind, package, file, line, COALESCE(signature, ''),
		        COALESCE(decorators, ''), COALESCE(base_classes, ''),
		        COALESCE(qual_name, ''), COALESCE(implements, '')
		 FROM symbols WHERE id = ?`, id,
	).Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Package, &sym.File, &sym.Line, &sym.Signature,
		&sym.Decorators, &sym.BaseClasses, &sym.QualName, &sym.Implements)
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
		`INSERT INTO files (path, language, size, content_hash, indexed_at, resolution)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   language = excluded.language,
		   size = excluded.size,
		   content_hash = excluded.content_hash,
		   indexed_at = excluded.indexed_at,
		   resolution = excluded.resolution`,
		f.Path, f.Language, f.Size, f.ContentHash, time.Now().Unix(), f.Resolution,
	)
	return err
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
		`SELECT id, name, kind, package, file, line, COALESCE(signature, ''),
		        COALESCE(decorators, ''), COALESCE(base_classes, ''),
		        COALESCE(qual_name, ''), COALESCE(implements, '')
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
		`SELECT id, name, kind, package, file, line, COALESCE(signature, ''),
		        COALESCE(decorators, ''), COALESCE(base_classes, ''),
		        COALESCE(qual_name, ''), COALESCE(implements, '')
		 FROM symbols WHERE package = ? ORDER BY file, line`, pkg,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// SearchSymbolsByName returns symbols whose name contains the query
// (case-insensitive). The query is literal: '%' and '_' match only
// themselves.
func (s *Store) SearchSymbolsByName(query string) ([]Symbol, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.db.Query(
		`SELECT id, name, kind, package, file, line, COALESCE(signature, ''),
		        COALESCE(decorators, ''), COALESCE(base_classes, ''),
		        COALESCE(qual_name, ''), COALESCE(implements, '')
		 FROM symbols WHERE name LIKE ? ESCAPE '\' ORDER BY name`,
		"%"+esc+"%",
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
		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.Package, &sym.File, &sym.Line, &sym.Signature,
			&sym.Decorators, &sym.BaseClasses, &sym.QualName, &sym.Implements); err != nil {
			return nil, err
		}
		syms = append(syms, sym)
	}
	return syms, rows.Err()
}

// GetCallableIDByName resolves a call target by name. Among symbols with
// that name it prefers a callable (function, method or class) over any other
// kind, so a call to get() does not land on an import or a var named get;
// among those it prefers one in preferFile, the caller's own file. A name
// with no callable still resolves to whatever has it, which keeps Go type
// conversions such as Celsius(x) as edges.
//
// A name resolves only to a symbol in preferFile's language (sameLanguage):
// a TypeScript call never lands on a Python function (G8 of #395), and a
// name looked up from a non-Go file never resolves to a Go symbol, which
// also keeps a full build, whose non-Go edges are resolved before the Go
// pass stores any Go symbol, and an update, which finds them stored, in
// agreement.
func (s *Store) GetCallableIDByName(name, preferFile string) (int64, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM symbols WHERE name = ?`+sameLanguage(preferFile)+`
		ORDER BY CASE WHEN kind IN ('function', 'method', 'class') THEN 0 ELSE 1 END,
			CASE WHEN file = ? THEN 0 ELSE 1 END,
			id
		LIMIT 1`, name, preferFile).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// GetSymbolIDByNameInFile returns the ID of the symbol with this name,
// preferring one declared in file over one stored first elsewhere. As in
// GetCallableIDByName, only a symbol in file's language matches.
func (s *Store) GetSymbolIDByNameInFile(name, file string) (int64, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM symbols WHERE name = ?`+sameLanguage(file)+`
		ORDER BY CASE WHEN file = ? THEN 0 ELSE 1 END, id
		LIMIT 1`, name, file).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// sameLanguage is the WHERE clause that keeps a name lookup from fromFile
// within its language family (languageFamily), by file extension. A file
// of no known language matches any symbol outside Go.
func sameLanguage(fromFile string) string {
	return languageClauses[languageFamily(DetectLanguage(fromFile))]
}

// languageFamily groups languages whose code calls into each other by
// name: TypeScript and JavaScript, and C and C++ (which share .h headers).
func languageFamily(lang string) string {
	switch lang {
	case "typescript":
		return "javascript"
	case "cpp":
		return "c"
	}
	return lang
}

// languageClauses maps a language family to its sameLanguage clause. The
// extensions come from extensionToLanguage, never from input.
var languageClauses = func() map[string]string {
	exts := map[string][]string{}
	for ext, lang := range extensionToLanguage {
		fam := languageFamily(lang)
		exts[fam] = append(exts[fam], strings.ToLower(ext))
	}
	out := make(map[string]string, len(exts))
	for fam, list := range exts {
		sort.Strings(list)
		var parts []string
		seen := map[string]bool{}
		for _, ext := range list {
			if !seen[ext] {
				seen[ext] = true
				parts = append(parts, `file LIKE '%`+ext+`'`)
			}
		}
		out[fam] = ` AND (` + strings.Join(parts, ` OR `) + `)`
	}
	out[""] = ` AND file NOT LIKE '%.go'`
	return out
}()

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
	rows, err := s.db.Query(`SELECT path, language, size, content_hash, indexed_at, COALESCE(resolution, '') FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.Path, &f.Language, &f.Size, &f.ContentHash, &f.IndexedAt, &f.Resolution); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// FindAllFunctionsWithEdges returns all functions/methods with their edge counts.
// Used by the unified code smell detector for single-pass analysis.
func (s *Store) FindAllFunctionsWithEdges() ([]FunctionEdgeInfo, error) {
	query := `
		SELECT s.name, s.file, s.line, s.kind, COALESCE(s.signature, ''),
		       COALESCE(s.decorators, ''), COALESCE(s.base_classes, ''),
		       COALESCE(s.implements, ''),
		       COALESCE((SELECT f.resolution FROM files f WHERE f.path = s.file), ''),
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
		if err := rows.Scan(&r.Name, &r.File, &r.Line, &r.Kind, &r.Signature,
			&r.Decorators, &r.BaseClasses, &r.Implements, &r.Resolution, &r.OutEdges, &r.InEdges); err != nil {
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
