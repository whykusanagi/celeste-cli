package codegraph

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBM25Idf_MonotonicInDF(t *testing.T) {
	// Rarer tokens (low DF) must get higher IDF than common tokens (high DF)
	// under a fixed corpus size. This is the property BM25 depends on.
	n := 1000
	rare := bm25Idf(2, n)
	common := bm25Idf(500, n)
	assert.Greater(t, rare, common, "rare tokens must outweigh common tokens")

	// All DFs in a well-formed corpus produce non-negative IDFs thanks
	// to the +1 inside the log.
	for df := 1; df <= n; df++ {
		idf := bm25Idf(df, n)
		assert.GreaterOrEqual(t, idf, 0.0, "bm25Idf should stay non-negative for df=%d", df)
	}
}

func TestBM25Idf_EdgeCases(t *testing.T) {
	assert.Equal(t, 0.0, bm25Idf(0, 100))
	assert.Equal(t, 0.0, bm25Idf(10, 0))
}

func TestComputeBM25Score_DiscriminativeTokenWins(t *testing.T) {
	// Two symbols, same length. Symbol A contains a rare query token
	// ("postgresql") that symbol B lacks. A must score higher.
	idf := map[string]float64{
		"postgresql": 4.0, // rare, high IDF
		"get":        0.1, // common, low IDF
	}
	avgDocLen := 10.0

	docA := map[string]int{"postgresql": 1, "get": 1}
	docB := map[string]int{"get": 1, "user": 1}
	query := []string{"postgresql", "get"}

	scoreA := ComputeBM25Score(query, docA, 2, idf, avgDocLen)
	scoreB := ComputeBM25Score(query, docB, 2, idf, avgDocLen)
	assert.Greater(t, scoreA, scoreB, "symbol with rare matched token must score higher")
}

func TestComputeBM25Score_ZeroWhenNoOverlap(t *testing.T) {
	idf := map[string]float64{"foo": 2.0}
	doc := map[string]int{"bar": 1, "baz": 1}
	assert.Equal(t, 0.0, ComputeBM25Score([]string{"foo"}, doc, 2, idf, 5.0))
}

func TestComputeBM25Score_LengthNormalization(t *testing.T) {
	// Two symbols with the same TF for the query token, but one is
	// much longer than average. The longer doc should get a LOWER score
	// (BM25 length normalization penalizes verbosity).
	idf := map[string]float64{"token": 2.0}
	avgDocLen := 10.0

	shortDoc := map[string]int{"token": 1}
	longDoc := map[string]int{"token": 1}
	query := []string{"token"}

	shortScore := ComputeBM25Score(query, shortDoc, 5, idf, avgDocLen)
	longScore := ComputeBM25Score(query, longDoc, 50, idf, avgDocLen)
	assert.Greater(t, shortScore, longScore, "short docs beat long docs at equal TF")
	// And both strictly positive because the token is present.
	assert.Greater(t, shortScore, 0.0)
	assert.Greater(t, longScore, 0.0)
	// The exact math should be finite.
	assert.False(t, math.IsNaN(shortScore))
	assert.False(t, math.IsInf(shortScore, 0))
}

func TestComputeFusedRanking_PrefersAgreement(t *testing.T) {
	// Three candidates. A ranks 1 in both lists, B ranks 2 in both,
	// C ranks 3 in both. Fused order must be A, B, C.
	jaccard := map[int64]int{1: 1, 2: 2, 3: 3}
	bm25 := map[int64]int{1: 1, 2: 2, 3: 3}
	order := ComputeFusedRanking(jaccard, bm25)
	assert.Equal(t, []int64{1, 2, 3}, order)
}

func TestComputeFusedRanking_DisagreementAndUnion(t *testing.T) {
	// Jaccard says A then B. BM25 says B then A. RRF with equal ranks
	// should tie A and B — tiebreak by lower symbol ID puts A first.
	jaccard := map[int64]int{10: 1, 20: 2}
	bm25 := map[int64]int{10: 2, 20: 1}
	order := ComputeFusedRanking(jaccard, bm25)
	assert.Equal(t, []int64{10, 20}, order)

	// If a symbol only appears in one list, it still makes it into the
	// fused output but scores lower than a symbol that shows up in both.
	jaccard2 := map[int64]int{1: 1}
	bm25_2 := map[int64]int{1: 1, 2: 1}
	order2 := ComputeFusedRanking(jaccard2, bm25_2)
	require.Len(t, order2, 2)
	assert.Equal(t, int64(1), order2[0], "agreeing symbol must beat single-list symbol")
	assert.Equal(t, int64(2), order2[1])
}

func TestComputeFusedRanking_EmptyInputs(t *testing.T) {
	order := ComputeFusedRanking(nil, nil)
	assert.Empty(t, order)
}

func TestRebuildTokenStats_EndToEnd(t *testing.T) {
	// Spin up a fresh store, write a few symbol token rows, rebuild the
	// stats table, and verify df/idf come out the way the formula predicts.
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "bm25.db"))
	require.NoError(t, err)
	defer store.Close()

	// Insert three dummy symbols with overlapping tokens.
	s1, err := store.UpsertSymbol(Symbol{Name: "A", Kind: SymbolFunction, File: "a.go"})
	require.NoError(t, err)
	s2, err := store.UpsertSymbol(Symbol{Name: "B", Kind: SymbolFunction, File: "b.go"})
	require.NoError(t, err)
	s3, err := store.UpsertSymbol(Symbol{Name: "C", Kind: SymbolFunction, File: "c.go"})
	require.NoError(t, err)

	require.NoError(t, store.UpsertSymbolTokens(s1, []string{"rare", "common"}))
	require.NoError(t, store.UpsertSymbolTokens(s2, []string{"common"}))
	require.NoError(t, store.UpsertSymbolTokens(s3, []string{"common"}))

	stats, err := store.RebuildTokenStats()
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 3, stats.NumDocs)
	// 4 total token-occurrences across 3 docs → avg doc length 4/3.
	assert.InDelta(t, 4.0/3.0, stats.AvgDocLength, 1e-9)

	// "rare" appears in 1 doc → higher IDF than "common" which appears in 3.
	idfs, err := store.GetIDFs([]string{"rare", "common"})
	require.NoError(t, err)
	assert.Greater(t, idfs["rare"], idfs["common"])
	assert.Greater(t, idfs["rare"], 0.0)

	// Cached meta row round-trips.
	cached, err := store.ReadBM25Stats()
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Equal(t, 3, cached.NumDocs)
	assert.InDelta(t, 4.0/3.0, cached.AvgDocLength, 1e-9)

	// GetSymbolTokens round-trips the TF counts.
	got, docLen, err := store.GetSymbolTokens(s1)
	require.NoError(t, err)
	assert.Equal(t, 1, got["rare"])
	assert.Equal(t, 1, got["common"])
	assert.Equal(t, 2, docLen)
}

func TestReadBM25Stats_EmptyWhenFreshStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "bm25.db"))
	require.NoError(t, err)
	defer store.Close()

	stats, err := store.ReadBM25Stats()
	require.NoError(t, err)
	assert.Nil(t, stats, "fresh store has no cached stats")
}
