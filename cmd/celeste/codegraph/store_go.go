package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Store methods used by the type-checked Go pass (gotypes.go). The Go pass
// rewrites every Go-sourced edge in one transaction, so these work in bulk.

// goSourceFilter selects symbols that live in Go files.
const goSourceFilter = `SELECT id FROM symbols WHERE file LIKE '%.go'`

// ResetGraph deletes every symbol, edge, file record and BM25/LSH row so a
// full Build starts from an empty graph, and stamps the current graph
// version and edge scope in the same transaction. Meta (MinHash seeds) and
// snapshots are kept. Dependent rows are deleted explicitly, children
// first, so the reset does not depend on ON DELETE CASCADE (which every
// connection now enforces, #389) and token_stats, which has no foreign
// key, is cleared too.
//
// Every row stored after the reset is written by this version, so the
// stamp is true from the moment the graph is empty. An update that finds
// the build unfinished (meta build_in_progress) then resumes it instead of
// taking the rows for an older index and dropping them (#394). The caller
// sets build_in_progress before the reset.
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
		if err := setMetaTx(tx, metaEdgeScope, []byte(edgeScope)); err != nil {
			return err
		}
		return setMetaTx(tx, metaGraphVersion, []byte(graphVersion))
	})
}

// UpgradeGraph turns an index written by an older graph version (or never
// stamped) into an unfinished build of the current one, in one
// transaction: it deletes every Go row (Go edges were resolved by name and
// the Go pass rewrites them all), marks every other file's record stale so
// the next pass parses it again with today's parsers, sets
// build_in_progress to token and stamps the current version and edge
// scope. The other languages' symbols and edges stay readable until each
// file is parsed again, and the update that finishes the build resolves every non-Go edge
// again, as a fresh build's pass 2 does (#394 G9). A run cut short leaves a
// current-version index with the mark set, which the next update resumes
// without dropping anything.
func (s *Store) UpgradeGraph(token string) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM edges WHERE source_id IN (` + goSourceFilter + `) OR target_id IN (` + goSourceFilter + `)`,
			`DELETE FROM lsh_bands WHERE symbol_id IN (` + goSourceFilter + `)`,
			`DELETE FROM symbol_tokens WHERE symbol_id IN (` + goSourceFilter + `)`,
			`DELETE FROM symbols WHERE file LIKE '%.go'`,
			`DELETE FROM files WHERE language = 'go' OR path LIKE '%.go'`,
			`UPDATE files SET content_hash = ''`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("upgrade index: %w", err)
			}
		}
		if err := setMetaTx(tx, metaBuildInProgress, []byte(token)); err != nil {
			return err
		}
		if err := setMetaTx(tx, metaEdgeScope, []byte(edgeScope)); err != nil {
			return err
		}
		return setMetaTx(tx, metaGraphVersion, []byte(graphVersion))
	})
}

// RescopeGraph turns a current-version index whose name-based edges were
// resolved under an older edge scope (or none: before #395 G8 a name could
// resolve to a symbol in another language) into an unfinished build, in
// one transaction: it sets build_in_progress to token and stamps the
// current edge scope. No row is deleted, so readers keep the graph; the
// update that finishes the build resolves every non-Go edge again and
// reruns the Go pass, and a run cut short leaves the mark for the next
// update to resume.
func (s *Store) RescopeGraph(token string) error {
	return s.inTx(func(tx *sql.Tx) error {
		if err := setMetaTx(tx, metaBuildInProgress, []byte(token)); err != nil {
			return err
		}
		return setMetaTx(tx, metaEdgeScope, []byte(edgeScope))
	})
}

func setMetaTx(tx *sql.Tx, key string, value []byte) error {
	if _, err := tx.Exec(
		"INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	); err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
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

// ReplaceNonGoEdges deletes every edge whose source is not a Go symbol and
// inserts edges in its place, in one transaction, so an interrupted build's
// non-Go edges are resolved again from scratch (#391) while a reader on
// another connection sees either the old edges or the new ones. ctx is
// checked every 1024 inserts; a cancel rolls the whole replacement back.
//
// The edges of symbols in the files listed in keep (files the caller could
// not parse again) are not deleted.
func (s *Store) ReplaceNonGoEdges(ctx context.Context, edges []Edge, keep []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// fail prefers the context's error, so a cancel that interrupts a
	// statement is reported as the cancel it is.
	fail := func(err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return err
	}
	del := `DELETE FROM edges WHERE source_id IN (SELECT id FROM symbols WHERE file NOT LIKE '%.go'`
	args := make([]any, 0, len(keep))
	if len(keep) > 0 {
		del += ` AND file NOT IN (?` + strings.Repeat(`, ?`, len(keep)-1) + `)`
		for _, f := range keep {
			args = append(args, f)
		}
	}
	del += `)`
	if _, err := tx.ExecContext(ctx, del, args...); err != nil {
		return fail(fmt.Errorf("clear non-go edges: %w", err))
	}
	if testHookReplaceNonGoAfterDelete != nil {
		testHookReplaceNonGoAfterDelete()
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, kind) VALUES (?, ?, ?)`)
	if err != nil {
		return fail(err)
	}
	defer stmt.Close()
	for i, e := range edges {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if _, err := stmt.ExecContext(ctx, e.SourceID, e.TargetID, e.Kind); err != nil {
			return fail(fmt.Errorf("insert non-go edge: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return nil
}

// fileEdge is a stored edge named by both ends' symbol names, scopes and
// files, so it can be stored again after either end's file is re-indexed
// (its symbol IDs change). The scope keeps same-named methods of different
// classes in one file apart.
type fileEdge struct {
	SourceName, SourceScope, SourceFile string
	TargetName, TargetScope, TargetFile string
	Kind                                EdgeKind
}

// incomingNonGoEdges returns the edges into file's symbols from symbols of
// other non-Go files: the edges DeleteFileSymbols(file) drops that those
// files' own rows do not record again.
func (s *Store) incomingNonGoEdges(file string) ([]fileEdge, error) {
	rows, err := s.db.Query(`
		SELECT src.name, COALESCE(src.scope, ''), src.file,
		       dst.name, COALESCE(dst.scope, ''), dst.file, e.kind
		FROM edges e
		JOIN symbols src ON src.id = e.source_id
		JOIN symbols dst ON dst.id = e.target_id
		WHERE dst.file = ? AND src.file <> ? AND src.file NOT LIKE '%.go'
		ORDER BY e.source_id, e.target_id`, file, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileEdge
	for rows.Next() {
		var e fileEdge
		if err := rows.Scan(&e.SourceName, &e.SourceScope, &e.SourceFile, &e.TargetName, &e.TargetScope, &e.TargetFile, &e.Kind); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// symbolIDInFile is the ID of the first symbol named name in scope (the
// class chain, "" for a top-level symbol) in exactly file.
func (s *Store) symbolIDInFile(name, scope, file string) (int64, bool) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM symbols WHERE name = ? AND COALESCE(scope, '') = ? AND file = ? ORDER BY line, id LIMIT 1`, name, scope, file).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// classRef is a stored class-like symbol: its name, scope, file and
// comma-separated base classes.
type classRef struct {
	name, scope, file, bases string
}

// classKinds are the symbol kinds a self call's hierarchy is made of.
const classKinds = `kind IN ('class', 'struct', 'interface', 'type')`

// classInFile is the class-like symbol named name in scope in exactly file.
func (s *Store) classInFile(name, scope, file string) (classRef, bool) {
	c := classRef{name: name, scope: scope, file: file}
	err := s.db.QueryRow(`SELECT COALESCE(base_classes, '') FROM symbols WHERE name = ? AND COALESCE(scope, '') = ? AND file = ? AND `+classKinds+` ORDER BY line, id LIMIT 1`, name, scope, file).Scan(&c.bases)
	return c, err == nil
}

// classByName is the class-like symbol named name in fromFile's language,
// one in fromFile preferred, then the first stored.
func (s *Store) classByName(name, fromFile string) (classRef, bool) {
	c := classRef{name: name}
	err := s.db.QueryRow(`SELECT COALESCE(scope, ''), file, COALESCE(base_classes, '') FROM symbols WHERE name = ? AND `+classKinds+sameLanguage(fromFile)+`
		ORDER BY CASE WHEN file = ? THEN 0 ELSE 1 END, id LIMIT 1`, name, fromFile).Scan(&c.scope, &c.file, &c.bases)
	return c, err == nil
}
