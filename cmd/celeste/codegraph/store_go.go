package codegraph

import (
	"database/sql"
	"fmt"
	"strings"
)

// Store methods used by the type-checked Go pass (gotypes.go). The Go pass
// rewrites every Go-sourced edge in one transaction, so these work in bulk.

// goSourceFilter selects symbols that live in Go files.
const goSourceFilter = `SELECT id FROM symbols WHERE file LIKE '%.go'`

// ResetGraph deletes every symbol, edge, file record and BM25/LSH row so a
// full Build starts from an empty graph. Meta (MinHash seeds) and snapshots
// are kept. Dependent rows are deleted explicitly rather than relying on
// ON DELETE CASCADE, which only applies on connections that enabled
// foreign_keys.
func (s *Store) ResetGraph() error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM edges`,
			`DELETE FROM lsh_bands`,
			`DELETE FROM symbol_tokens`,
			`DELETE FROM token_stats`,
			`DELETE FROM symbols`,
			`DELETE FROM files`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("reset graph: %w", err)
			}
		}
		return nil
	})
}

// ResetGo deletes every Go symbol, file record and the edges and BM25/LSH
// rows attached to Go symbols, in one transaction. Rows for other languages
// are kept: an index upgrade only changes how Go is resolved.
func (s *Store) ResetGo() error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM edges WHERE source_id IN (` + goSourceFilter + `) OR target_id IN (` + goSourceFilter + `)`,
			`DELETE FROM lsh_bands WHERE symbol_id IN (` + goSourceFilter + `)`,
			`DELETE FROM symbol_tokens WHERE symbol_id IN (` + goSourceFilter + `)`,
			`DELETE FROM symbols WHERE file LIKE '%.go'`,
			`DELETE FROM files WHERE language = 'go' OR path LIKE '%.go'`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("reset go rows: %w", err)
			}
		}
		return nil
	})
}

// GoQualIDs maps each qualified Go name to its symbol ID. When two rows
// share a qualified name (two init functions in one file) the
// lowest ID wins, so the mapping is deterministic.
func (s *Store) GoQualIDs() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT id, qual_name FROM symbols WHERE qual_name IS NOT NULL AND qual_name != '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var id int64
		var q string
		if err := rows.Scan(&id, &q); err != nil {
			return nil, err
		}
		if _, ok := out[q]; !ok {
			out[q] = id
		}
	}
	return out, rows.Err()
}

// ReplaceGoEdges deletes every edge whose source is a Go symbol and inserts
// edges in its place, in one transaction.
func (s *Store) ReplaceGoEdges(edges []Edge) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM edges WHERE source_id IN (` + goSourceFilter + `)`); err != nil {
			return fmt.Errorf("clear go edges: %w", err)
		}
		stmt, err := tx.Prepare(`INSERT OR IGNORE INTO edges (source_id, target_id, kind) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range edges {
			if _, err := stmt.Exec(e.SourceID, e.TargetID, e.Kind); err != nil {
				return fmt.Errorf("insert go edge: %w", err)
			}
		}
		return nil
	})
}

// SetGoImplements clears symbols.implements on every Go symbol and then
// sets it for the given symbol IDs, in one transaction.
func (s *Store) SetGoImplements(impl map[int64]string) error {
	return s.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE symbols SET implements = NULL WHERE file LIKE '%.go' AND implements IS NOT NULL`); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`UPDATE symbols SET implements = ? WHERE id = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for id, v := range impl {
			if _, err := stmt.Exec(v, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetFileResolutions records the Go resolution of already-indexed files.
func (s *Store) SetFileResolutions(res map[string]string) error {
	return s.inTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`UPDATE files SET resolution = ? WHERE path = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for path, r := range res {
			if _, err := stmt.Exec(r, path); err != nil {
				return err
			}
		}
		return nil
	})
}

// FileResolutions returns path -> resolution for every file that has one
// (Go files only).
func (s *Store) FileResolutions() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT path, resolution FROM files WHERE resolution IS NOT NULL AND resolution != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var p, r string
		if err := rows.Scan(&p, &r); err != nil {
			return nil, err
		}
		out[p] = r
	}
	return out, rows.Err()
}

// FileResolution returns the Go resolution recorded for one file, or "".
func (s *Store) FileResolution(path string) string {
	var r sql.NullString
	if err := s.db.QueryRow(`SELECT resolution FROM files WHERE path = ?`, path).Scan(&r); err != nil {
		return ""
	}
	return r.String
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DisplayName is a symbol's name for listings. Go methods and interface
// methods with a qualified name show their receiver, "(*codegraph.Indexer).Build",
// so same-named methods on different types stay distinguishable; every
// other symbol shows its plain name.
func DisplayName(sym Symbol) string {
	if sym.Kind != SymbolMethod && sym.Kind != SymbolInterfaceMethod {
		return sym.Name
	}
	q := sym.QualName
	end := strings.LastIndex(q, ").")
	if !strings.HasPrefix(q, "(") || end < 0 {
		return sym.Name
	}
	recv := q[1:end]
	star := ""
	if strings.HasPrefix(recv, "*") {
		star, recv = "*", recv[1:]
	}
	if i := strings.LastIndex(recv, "/"); i >= 0 {
		recv = recv[i+1:]
	}
	return "(" + star + recv + ")." + sym.Name
}
