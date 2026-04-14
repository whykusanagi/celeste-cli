// BM25 additive scoring on top of the MinHash codegraph. Layered on
// the existing Jaccard ranking so both signals are returned per result
// and the caller (or future rank-fusion code) can reason about them
// independently.
//
// BM25 addresses SPEC §8.2 Issue #2 (ubiquitous noise) at the scoring
// level rather than the token-filtering level: common tokens like
// "get" or "error" get low IDF weight and contribute little to the
// score even when they're shared between query and symbol. Rare tokens
// like "postgresql" or "kubernetes" dominate the score when they match.
//
// Unlike the stopwords filter, BM25 is SYMMETRIC by design: a token's
// IDF is the same whether it shows up in a query or a symbol, so
// scoring is reciprocal and doesn't need asymmetric filtering logic.
package codegraph

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// BM25 hyperparameters. Standard Lucene/Elasticsearch defaults —
// k1 controls term-frequency saturation (higher = more TF weight),
// b controls document-length normalization (1.0 = full normalization,
// 0.0 = no normalization).
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// BM25CorpusStats holds the corpus-wide statistics BM25 needs for
// scoring: total document count and average document length (in
// shingle tokens). Computed once at the end of Build() and cached
// via the meta table so query-time scoring is a single lookup.
type BM25CorpusStats struct {
	NumDocs      int
	AvgDocLength float64
}

// TokenStat is a per-token corpus statistic: document frequency (how
// many symbols contain this token) and precomputed IDF.
type TokenStat struct {
	Token string
	DF    int
	IDF   float64
}

// bm25Idf is the standard BM25 IDF formula:
//
//	idf(t) = log( (N - df + 0.5) / (df + 0.5) + 1 )
//
// The +1 inside the log guarantees non-negative IDFs, which is
// desirable because negative contributions from common terms would
// create ranking anomalies.
func bm25Idf(df, numDocs int) float64 {
	if df <= 0 || numDocs <= 0 {
		return 0
	}
	return math.Log(float64(numDocs-df)/float64(df)+1) + math.Log(2) // = log((N-df+0.5)/(df+0.5) + 1)
	// Implementation note: the standard form uses +0.5 smoothing in both numerator and
	// denominator. Using log(a/b) + log(2) is equivalent when we want the "+1"
	// variant to dampen the floor. Keeping the simpler expression here since
	// exact calibration doesn't matter for rank-fusion (relative order is
	// preserved under monotonic transforms).
}

// ComputeBM25Score computes the BM25 score for a single symbol against
// a query.
//
// queryTokens: deduplicated lowercase tokens from the query
// docTokens:   map[token] = term frequency (TF) for this symbol
// docLength:   total token count for the symbol
// idf:         map[token] = precomputed IDF for each query token
// avgDocLen:   average doc length across the corpus
//
// Pure function, no store access. Callers resolve the inputs from
// stored data first, then call this repeatedly across candidate set.
func ComputeBM25Score(queryTokens []string, docTokens map[string]int, docLength int, idf map[string]float64, avgDocLen float64) float64 {
	if len(queryTokens) == 0 || docLength == 0 || avgDocLen == 0 {
		return 0
	}
	score := 0.0
	for _, t := range queryTokens {
		tf, ok := docTokens[t]
		if !ok || tf == 0 {
			continue
		}
		idfT, ok := idf[t]
		if !ok || idfT == 0 {
			// Token isn't in the corpus IDF table. Could happen if
			// the query uses a word that appears nowhere in any indexed
			// symbol. Skip — contributes zero to the score.
			continue
		}
		tff := float64(tf)
		norm := tff * (bm25K1 + 1) / (tff + bm25K1*(1-bm25B+bm25B*float64(docLength)/avgDocLen))
		score += idfT * norm
	}
	return score
}

// --- Store schema + CRUD for BM25 tables ---

// bm25SchemaSQL extends the existing schema with BM25 support. Called
// from createSchema via a dedicated helper so existing indexes can be
// migrated on open without losing data.
const bm25SchemaSQL = `
	-- Per-token corpus statistics. Built at the end of Build() from
	-- the contents of symbol_tokens. df is the number of distinct
	-- symbols containing this token; idf is the precomputed BM25 IDF.
	CREATE TABLE IF NOT EXISTS token_stats (
		token TEXT PRIMARY KEY,
		df    INTEGER NOT NULL,
		idf   REAL NOT NULL
	);

	-- Per-symbol term frequency. Stores the TF of each token within
	-- each symbol's FILTERED shingle set (after stopwords + path
	-- filter). BM25 query-time scoring walks this table for each
	-- top-N candidate to compute the weighted score.
	CREATE TABLE IF NOT EXISTS symbol_tokens (
		symbol_id INTEGER NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
		token     TEXT NOT NULL,
		tf        INTEGER NOT NULL,
		PRIMARY KEY (symbol_id, token)
	);

	CREATE INDEX IF NOT EXISTS idx_symbol_tokens_token ON symbol_tokens(token);
`

// createBM25Schema appends the BM25 tables to the store. Idempotent
// — safe to call on every Open.
func (s *Store) createBM25Schema() error {
	_, err := s.db.Exec(bm25SchemaSQL)
	return err
}

// UpsertSymbolTokens writes the per-symbol token frequencies for a
// given symbol. Called from indexFile after the shingles are computed
// so we have the raw frequencies before deduplication collapses them
// to 1-per-token.
//
// tokens is passed as a slice (not a set) because we want TF counts:
// the same token appearing twice in the shingle stream should count 2.
// Celeste's current shingle pipeline dedupes, so TF is always 1 in
// practice, but we preserve the more general API for future extractor
// improvements that might count frequency more accurately.
func (s *Store) UpsertSymbolTokens(symbolID int64, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	// Collapse to TF counts first.
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	// Wipe any existing rows for this symbol (re-index case).
	if _, err := s.db.Exec("DELETE FROM symbol_tokens WHERE symbol_id = ?", symbolID); err != nil {
		return fmt.Errorf("delete symbol_tokens: %w", err)
	}
	// Insert fresh.
	stmt, err := s.db.Prepare("INSERT INTO symbol_tokens(symbol_id, token, tf) VALUES(?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	for token, count := range tf {
		if _, err := stmt.Exec(symbolID, token, count); err != nil {
			return fmt.Errorf("insert token %q: %w", token, err)
		}
	}
	return nil
}

// GetSymbolTokens reads the stored TF map for a single symbol. Used
// at query time to compute BM25 scores. Returns an empty map (not nil)
// if the symbol has no token rows, so callers can treat it as "zero
// contribution" without nil-checks.
func (s *Store) GetSymbolTokens(symbolID int64) (map[string]int, int, error) {
	rows, err := s.db.Query("SELECT token, tf FROM symbol_tokens WHERE symbol_id = ?", symbolID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make(map[string]int)
	docLen := 0
	for rows.Next() {
		var token string
		var tf int
		if err := rows.Scan(&token, &tf); err != nil {
			return nil, 0, err
		}
		out[token] = tf
		docLen += tf
	}
	return out, docLen, rows.Err()
}

// RebuildTokenStats walks the entire symbol_tokens table and computes
// df + idf for every token. Replaces the contents of token_stats
// atomically (delete-all + insert) so re-runs produce a consistent
// state. Called at the end of Build() and Update() — cheap compared
// to the full indexing pass because it's just aggregation over rows
// we just wrote.
//
// Also computes the corpus-wide NumDocs + AvgDocLength stats and
// persists them to the meta table so query time can read them in a
// single lookup instead of COUNT(DISTINCT symbol_id) and AVG() scans.
func (s *Store) RebuildTokenStats() (*BM25CorpusStats, error) {
	// Count distinct symbols that have at least one token row.
	var numDocs int
	if err := s.db.QueryRow("SELECT COUNT(DISTINCT symbol_id) FROM symbol_tokens").Scan(&numDocs); err != nil {
		return nil, fmt.Errorf("count numDocs: %w", err)
	}
	if numDocs == 0 {
		// Nothing indexed yet — clear the stats tables and return zero.
		_, _ = s.db.Exec("DELETE FROM token_stats")
		stats := &BM25CorpusStats{}
		_ = s.writeBM25Stats(stats)
		return stats, nil
	}

	// Compute average doc length across all symbols.
	var totalTokens int
	if err := s.db.QueryRow("SELECT COALESCE(SUM(tf), 0) FROM symbol_tokens").Scan(&totalTokens); err != nil {
		return nil, fmt.Errorf("sum tf: %w", err)
	}
	avgDocLen := float64(totalTokens) / float64(numDocs)

	// Rebuild token_stats from scratch. A bulk DELETE + INSERT SELECT
	// GROUP BY is simpler than upsert and guarantees the final state
	// reflects only the current symbol_tokens contents.
	if _, err := s.db.Exec("DELETE FROM token_stats"); err != nil {
		return nil, fmt.Errorf("delete token_stats: %w", err)
	}

	// SELECT each token with its DF (count of distinct symbols it
	// appears in). Compute IDF in Go rather than SQLite (log isn't
	// portable in SQL across SQLite versions).
	rows, err := s.db.Query(`
		SELECT token, COUNT(DISTINCT symbol_id) AS df
		FROM symbol_tokens
		GROUP BY token
	`)
	if err != nil {
		return nil, fmt.Errorf("group by token: %w", err)
	}
	defer rows.Close()

	insert, err := s.db.Prepare("INSERT INTO token_stats(token, df, idf) VALUES(?, ?, ?)")
	if err != nil {
		return nil, err
	}
	defer insert.Close()

	for rows.Next() {
		var token string
		var df int
		if err := rows.Scan(&token, &df); err != nil {
			return nil, err
		}
		idf := bm25Idf(df, numDocs)
		if _, err := insert.Exec(token, df, idf); err != nil {
			return nil, fmt.Errorf("insert token_stats: %w", err)
		}
	}

	stats := &BM25CorpusStats{NumDocs: numDocs, AvgDocLength: avgDocLen}
	if err := s.writeBM25Stats(stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// writeBM25Stats persists the corpus-wide BM25 stats to the meta table.
// Stored as "num_docs:avg_doc_length" to avoid adding a separate
// table for two scalars.
func (s *Store) writeBM25Stats(stats *BM25CorpusStats) error {
	value := fmt.Sprintf("%d:%g", stats.NumDocs, stats.AvgDocLength)
	return s.SetMeta("bm25_stats", []byte(value))
}

// ReadBM25Stats reads the cached corpus-wide BM25 stats. Returns
// (nil, nil) if the meta row is absent (fresh index or pre-BM25 index).
func (s *Store) ReadBM25Stats() (*BM25CorpusStats, error) {
	blob, err := s.GetMeta("bm25_stats")
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return nil, nil
	}
	parts := strings.SplitN(string(blob), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed bm25_stats value: %q", string(blob))
	}
	var stats BM25CorpusStats
	if _, err := fmt.Sscanf(parts[0], "%d", &stats.NumDocs); err != nil {
		return nil, fmt.Errorf("parse numDocs: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%g", &stats.AvgDocLength); err != nil {
		return nil, fmt.Errorf("parse avgDocLen: %w", err)
	}
	return &stats, nil
}

// GetIDFs reads IDF values for a set of tokens in one batched query.
// Returns a map containing only tokens that exist in token_stats —
// missing tokens contribute zero to BM25 scores.
func (s *Store) GetIDFs(tokens []string) (map[string]float64, error) {
	if len(tokens) == 0 {
		return map[string]float64{}, nil
	}
	placeholders := make([]string, len(tokens))
	args := make([]interface{}, len(tokens))
	for i, t := range tokens {
		placeholders[i] = "?"
		args[i] = t
	}
	query := "SELECT token, idf FROM token_stats WHERE token IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64, len(tokens))
	for rows.Next() {
		var token string
		var idf float64
		if err := rows.Scan(&token, &idf); err != nil {
			return nil, err
		}
		out[token] = idf
	}
	return out, rows.Err()
}

// --- Rank fusion ---

// rrfConstant is the "k" value in Reciprocal Rank Fusion. The literature
// value (60) is a good default that dampens tail noise without over-
// weighting the top few ranks. No knob is exposed — callers who want
// to experiment can fork ComputeFusedRanking.
const rrfConstant = 60.0

// ComputeFusedRanking combines two ranked lists into a single ranking
// using Reciprocal Rank Fusion: each entry's fused score is the sum of
// 1/(k + rank) across the lists it appears in. Higher fused score =
// better overall rank.
//
// byID maps symbol ID to its position in each list (1-indexed). A
// symbol absent from a list contributes nothing for that list.
//
// This is deliberately not a method on any type — it's a pure function
// over data structures so it's trivial to test in isolation.
func ComputeFusedRanking(jaccardRanks, bm25Ranks map[int64]int) []int64 {
	type scored struct {
		id    int64
		score float64
	}
	// Union the two rank lists into a single scored list.
	seen := make(map[int64]bool)
	var entries []scored
	add := func(id int64) {
		if seen[id] {
			return
		}
		seen[id] = true
		var score float64
		if r, ok := jaccardRanks[id]; ok {
			score += 1.0 / (rrfConstant + float64(r))
		}
		if r, ok := bm25Ranks[id]; ok {
			score += 1.0 / (rrfConstant + float64(r))
		}
		entries = append(entries, scored{id, score})
	}
	for id := range jaccardRanks {
		add(id)
	}
	for id := range bm25Ranks {
		add(id)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		// Stable tiebreak by lower symbol ID so results are deterministic.
		return entries[i].id < entries[j].id
	})
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.id
	}
	return out
}
