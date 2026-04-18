// LSH (Locality-Sensitive Hashing) band table for sub-linear semantic
// search. Implements the persisted band table approach validated in
// CODEGRAPH_LSH_RESEARCH.md — instead of loading all MinHash signatures
// and comparing exhaustively (O(N)), precompute band hashes at index
// time and query them at search time to retrieve a small candidate set
// (typically 0.1-1% of the corpus) that is then ranked by exact Jaccard
// similarity.
//
// Band configuration: 64 bands × 2 rows per band from the 128-element
// MinHash signature. This is the empirically-derived "64×2" config from
// the research doc — counterintuitive from a classical LSH perspective
// (it produces large candidate sets) but necessary because code search
// operates at much lower Jaccard values (0.05-0.20) than document
// similarity (0.5-0.8). The conventional 16×8 and 32×4 configs produce
// 0% recall on code search queries.
//
// At grafana scale (77,420 symbols), this provides a 20x speedup over
// brute-force (89ms → 4.3ms) by eliminating the full signature load.
package codegraph

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// LSH band configuration. 64 bands of 2 hash elements each covers the
// full 128-element MinHash signature. Empirically validated — see
// CODEGRAPH_LSH_RESEARCH.md §3 Claim 2.
const (
	lshNumBands = 64
	lshBandSize = 2 // elements per band; lshNumBands * lshBandSize must == DefaultNumHashes (128)
)

// lshSchemaSQL creates the persisted band table. Called from
// createSchema via createLSHSchema. Idempotent.
const lshSchemaSQL = `
	CREATE TABLE IF NOT EXISTS lsh_bands (
		band_id    INTEGER NOT NULL,
		band_hash  INTEGER NOT NULL,
		symbol_id  INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_lsh_bands_lookup ON lsh_bands(band_id, band_hash);
	CREATE INDEX IF NOT EXISTS idx_lsh_bands_symbol ON lsh_bands(symbol_id);
`

// createLSHSchema adds the LSH tables to the store. Idempotent.
func (s *Store) createLSHSchema() error {
	_, err := s.db.Exec(lshSchemaSQL)
	return err
}

// ComputeBandHashes splits a 128-element MinHash signature into 64
// bands of 2 elements each and hashes each band to a single uint64.
// The band hash is computed by XORing the two elements with a
// band-specific salt — cheap and effective for LSH purposes where
// we only need the property that identical input bands produce
// identical hashes.
//
// Returns exactly lshNumBands (64) hashes. Panics if len(sig) !=
// lshNumBands * lshBandSize — callers must pass a full signature.
func ComputeBandHashes(sig MinHashSignature) []uint64 {
	expected := lshNumBands * lshBandSize
	if len(sig) != expected {
		panic(fmt.Sprintf("ComputeBandHashes: sig length %d != expected %d", len(sig), expected))
	}
	bands := make([]uint64, lshNumBands)
	for b := 0; b < lshNumBands; b++ {
		offset := b * lshBandSize
		// Combine the two elements. Use a simple scheme: hash the
		// concatenated little-endian bytes with FNV-1a seeded by
		// the band index. This gives us collision resistance across
		// bands while being fast.
		var buf [16]byte // 2 × 8 bytes
		binary.LittleEndian.PutUint64(buf[0:8], sig[offset])
		binary.LittleEndian.PutUint64(buf[8:16], sig[offset+1])
		// FNV-1a with band index as seed offset
		h := uint64(14695981039346656037) ^ uint64(b)*6364136223846793005
		for _, c := range buf {
			h ^= uint64(c)
			h *= 1099511628211 // FNV prime
		}
		bands[b] = h
	}
	return bands
}

// UpsertLSHBands writes the band hashes for a single symbol. Deletes
// any existing rows first (re-index case) then bulk-inserts 64 rows.
func (s *Store) UpsertLSHBands(symbolID int64, bands []uint64) error {
	if len(bands) == 0 {
		return nil
	}
	// Delete existing rows for this symbol (re-index).
	if _, err := s.db.Exec("DELETE FROM lsh_bands WHERE symbol_id = ?", symbolID); err != nil {
		return fmt.Errorf("delete lsh_bands: %w", err)
	}
	stmt, err := s.db.Prepare("INSERT INTO lsh_bands(band_id, band_hash, symbol_id) VALUES(?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare lsh insert: %w", err)
	}
	defer stmt.Close()
	for bandID, hash := range bands {
		// Store band_hash as signed int64 (SQLite INTEGER is always
		// signed) — the bit pattern is preserved and the lookup
		// works correctly because we compare exact values.
		if _, err := stmt.Exec(bandID, int64(hash), symbolID); err != nil {
			return fmt.Errorf("insert band %d: %w", bandID, err)
		}
	}
	return nil
}

// QueryLSHCandidates retrieves the set of symbol IDs that share at
// least one band hash with the query. This is the LSH candidate set —
// typically 0.1-1% of the corpus — which is then ranked by exact
// Jaccard similarity.
//
// The query is a single SELECT with OR clauses across all 64 bands:
//
//	SELECT DISTINCT symbol_id FROM lsh_bands
//	WHERE (band_id = 0 AND band_hash = ?) OR (band_id = 1 AND band_hash = ?) OR ...
//
// The index on (band_id, band_hash) makes each OR branch an O(1)
// lookup. DISTINCT collapses symbols that match on multiple bands.
func (s *Store) QueryLSHCandidates(queryBands []uint64) ([]int64, error) {
	if len(queryBands) == 0 {
		return nil, nil
	}
	// Build the OR clauses.
	clauses := make([]string, len(queryBands))
	args := make([]interface{}, len(queryBands)*2)
	for i, hash := range queryBands {
		clauses[i] = "(band_id = ? AND band_hash = ?)"
		args[i*2] = i
		args[i*2+1] = int64(hash)
	}
	query := "SELECT DISTINCT symbol_id FROM lsh_bands WHERE " + strings.Join(clauses, " OR ")
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query lsh candidates: %w", err)
	}
	defer rows.Close()
	var candidates []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		candidates = append(candidates, id)
	}
	return candidates, rows.Err()
}

// HasLSHData returns true if the lsh_bands table has at least one row.
// Used to decide whether to use the LSH path or fall back to brute-force
// at query time — pre-LSH indexes have no band data and must still work.
func (s *Store) HasLSHData() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM lsh_bands LIMIT 1").Scan(&count)
	return err == nil && count > 0
}
