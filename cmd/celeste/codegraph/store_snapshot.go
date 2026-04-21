// Store methods for graph snapshots and change impact analysis.
package codegraph

import (
	"database/sql"
	"fmt"
	"time"
)

// ensureSnapshotsTable creates the snapshots table if it doesn't exist.
func (s *Store) ensureSnapshotsTable() {
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS snapshots (
		commit_sha TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		data BLOB NOT NULL
	)`)
}

// AllSymbolNamesAndKinds returns a map of symbol name → kind for all symbols.
func (s *Store) AllSymbolNamesAndKinds() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT name, kind FROM symbols`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			continue
		}
		result[name] = kind
	}
	return result, nil
}

// AllEdgeKeys returns a list of "source→target:kind" strings for all edges.
func (s *Store) AllEdgeKeys() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT s1.name, s2.name, e.kind
		FROM edges e
		JOIN symbols s1 ON e.source_id = s1.id
		JOIN symbols s2 ON e.target_id = s2.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var source, target, kind string
		if err := rows.Scan(&source, &target, &kind); err != nil {
			continue
		}
		keys = append(keys, fmt.Sprintf("%s→%s:%s", source, target, kind))
	}
	return keys, nil
}

// SaveSnapshot persists a graph snapshot.
func (s *Store) SaveSnapshot(commitSHA string, ts time.Time, data []byte) error {
	s.ensureSnapshotsTable()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO snapshots (commit_sha, created_at, data) VALUES (?, ?, ?)`,
		commitSHA, ts.Unix(), data)
	return err
}

// LoadSnapshot retrieves a snapshot by commit SHA.
func (s *Store) LoadSnapshot(commitSHA string) ([]byte, error) {
	s.ensureSnapshotsTable()
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM snapshots WHERE commit_sha = ?`, commitSHA).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot not found: %s", commitSHA)
	}
	return data, err
}

// LatestSnapshot returns the most recent snapshot data, or nil if none exist.
func (s *Store) LatestSnapshot() ([]byte, error) {
	s.ensureSnapshotsTable()
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM snapshots ORDER BY created_at DESC LIMIT 1`).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

// SymbolsInLineRange returns symbols in a file whose line range overlaps [startLine, endLine].
func (s *Store) SymbolsInLineRange(file string, startLine, endLine int) []Symbol {
	rows, err := s.db.Query(`
		SELECT name, kind, file, line, COALESCE(signature, '')
		FROM symbols
		WHERE file = ? AND line >= ? AND line <= ?
	`, file, startLine, endLine)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var syms []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.Name, &sym.Kind, &sym.File, &sym.Line, &sym.Signature); err != nil {
			continue
		}
		syms = append(syms, sym)
	}
	return syms
}

// CallersOf returns symbols that have a "calls" edge targeting the named symbol.
func (s *Store) CallersOf(targetName string) []Symbol {
	rows, err := s.db.Query(`
		SELECT s1.name, s1.kind, s1.file, s1.line, COALESCE(s1.signature, '')
		FROM edges e
		JOIN symbols s1 ON e.source_id = s1.id
		JOIN symbols s2 ON e.target_id = s2.id
		WHERE s2.name = ? AND e.kind = 'calls'
	`, targetName)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var syms []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.Name, &sym.Kind, &sym.File, &sym.Line, &sym.Signature); err != nil {
			continue
		}
		syms = append(syms, sym)
	}
	return syms
}

// CallerCount returns the number of symbols that call the named symbol.
func (s *Store) CallerCount(targetName string) int {
	var count int
	s.db.QueryRow(`
		SELECT COUNT(DISTINCT e.source_id)
		FROM edges e
		JOIN symbols s2 ON e.target_id = s2.id
		WHERE s2.name = ? AND e.kind = 'calls'
	`, targetName).Scan(&count) //nolint:errcheck // best-effort count
	return count
}

// EdgeCount returns the total number of edges connected to a symbol.
func (s *Store) EdgeCount(name string) int {
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*)
		FROM edges e
		JOIN symbols s ON (e.source_id = s.id OR e.target_id = s.id)
		WHERE s.name = ?
	`, name).Scan(&count)
	return count
}

// HasTestCoverage returns true if a symbol has any callers from test files.
func (s *Store) HasTestCoverage(name string) bool {
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*)
		FROM edges e
		JOIN symbols s1 ON e.source_id = s1.id
		JOIN symbols s2 ON e.target_id = s2.id
		WHERE s2.name = ? AND e.kind = 'calls'
		AND (s1.file LIKE '%_test.go' OR s1.file LIKE '%_test.py'
			OR s1.file LIKE '%.test.ts' OR s1.file LIKE '%.test.js'
			OR s1.file LIKE '%.spec.ts' OR s1.file LIKE '%.spec.js'
			OR s1.file LIKE '%_test.rs')
	`, name).Scan(&count)
	return count > 0
}
