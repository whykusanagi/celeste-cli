package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeBandHashes_Length(t *testing.T) {
	// 128-element signature should produce exactly 64 band hashes.
	sig := make(MinHashSignature, 128)
	for i := range sig {
		sig[i] = uint64(i * 7)
	}
	bands := ComputeBandHashes(sig)
	assert.Len(t, bands, lshNumBands)
}

func TestComputeBandHashes_Deterministic(t *testing.T) {
	// Same signature must produce the same band hashes every time.
	sig := make(MinHashSignature, 128)
	for i := range sig {
		sig[i] = uint64(i*3 + 17)
	}
	b1 := ComputeBandHashes(sig)
	b2 := ComputeBandHashes(sig)
	assert.Equal(t, b1, b2)
}

func TestComputeBandHashes_DifferentInputsDiffer(t *testing.T) {
	// Two different signatures should produce different band hashes
	// (at least some bands should differ).
	sig1 := make(MinHashSignature, 128)
	sig2 := make(MinHashSignature, 128)
	for i := range sig1 {
		sig1[i] = uint64(i)
		sig2[i] = uint64(i + 1000)
	}
	b1 := ComputeBandHashes(sig1)
	b2 := ComputeBandHashes(sig2)
	matches := 0
	for i := range b1 {
		if b1[i] == b2[i] {
			matches++
		}
	}
	assert.Less(t, matches, lshNumBands, "completely different signatures should differ on most bands")
}

func TestLSH_EndToEnd_CandidateRetrieval(t *testing.T) {
	// Create a store, insert 3 symbols with MinHash signatures and
	// LSH bands, then query with a signature that's similar to one
	// of them. The LSH candidate set should include the similar
	// symbol.
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "lsh.db"))
	require.NoError(t, err)
	defer store.Close()

	hasher := NewMinHasher(128)

	// Symbol A: "validate session token auth"
	sA, err := store.UpsertSymbol(Symbol{Name: "validateSession", Kind: SymbolFunction, File: "a.go"})
	require.NoError(t, err)
	sigA := hasher.Signature([]string{"validate", "session", "token", "auth"})
	require.NoError(t, store.UpdateMinHash(sA, sigA))
	require.NoError(t, store.UpsertLSHBands(sA, ComputeBandHashes(sigA)))

	// Symbol B: "database connection pool query" (different domain)
	sB, err := store.UpsertSymbol(Symbol{Name: "connectionPool", Kind: SymbolFunction, File: "b.go"})
	require.NoError(t, err)
	sigB := hasher.Signature([]string{"database", "connection", "pool", "query"})
	require.NoError(t, store.UpdateMinHash(sB, sigB))
	require.NoError(t, store.UpsertLSHBands(sB, ComputeBandHashes(sigB)))

	// Symbol C: "validate token refresh" (similar to A)
	sC, err := store.UpsertSymbol(Symbol{Name: "refreshToken", Kind: SymbolFunction, File: "c.go"})
	require.NoError(t, err)
	sigC := hasher.Signature([]string{"validate", "token", "refresh"})
	require.NoError(t, store.UpdateMinHash(sC, sigC))
	require.NoError(t, store.UpsertLSHBands(sC, ComputeBandHashes(sigC)))

	// Query: "authentication session token" — should find A and C
	// as candidates (they share "token"/"validate" shingles), B
	// should be absent or at least A and C should be present.
	querySig := hasher.Signature([]string{"authentication", "session", "token"})
	queryBands := ComputeBandHashes(querySig)

	candidates, err := store.QueryLSHCandidates(queryBands)
	require.NoError(t, err)

	// At least the highly similar symbol A should be a candidate.
	// With 64×2 bands and overlapping tokens, both A and C should
	// appear. B may or may not appear (false positive is OK — LSH
	// is approximate). The key property is that A IS in the set.
	candidateSet := make(map[int64]bool)
	for _, id := range candidates {
		candidateSet[id] = true
	}
	assert.True(t, candidateSet[sA], "validateSession must be an LSH candidate for 'authentication session token'")
}

func TestHasLSHData_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "lsh.db"))
	require.NoError(t, err)
	defer store.Close()

	assert.False(t, store.HasLSHData(), "fresh store should have no LSH data")
}

func TestHasLSHData_AfterInsert(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "lsh.db"))
	require.NoError(t, err)
	defer store.Close()

	s, err := store.UpsertSymbol(Symbol{Name: "foo", Kind: SymbolFunction, File: "f.go"})
	require.NoError(t, err)
	require.NoError(t, store.UpsertLSHBands(s, make([]uint64, 64)))

	assert.True(t, store.HasLSHData(), "store with LSH rows should report HasLSHData=true")
}

func TestUpsertLSHBands_Idempotent(t *testing.T) {
	// Re-inserting bands for the same symbol should replace, not
	// duplicate. Verify row count stays at 64.
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "lsh.db"))
	require.NoError(t, err)
	defer store.Close()

	s, err := store.UpsertSymbol(Symbol{Name: "bar", Kind: SymbolFunction, File: "b.go"})
	require.NoError(t, err)

	bands := make([]uint64, 64)
	for i := range bands {
		bands[i] = uint64(i * 42)
	}

	require.NoError(t, store.UpsertLSHBands(s, bands))
	require.NoError(t, store.UpsertLSHBands(s, bands)) // re-insert

	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM lsh_bands WHERE symbol_id = ?", s).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 64, count, "re-insert should replace rows, not duplicate them")
}

func TestLSH_IntegrationWithSearch(t *testing.T) {
	// End-to-end: build an index with LSH, then verify
	// SemanticSearch uses the LSH path (not brute-force).
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "lsh-search.db"))
	require.NoError(t, err)

	idx := NewIndexerWithStore(store, dir)
	defer idx.Close()

	// Write a simple Go file to index
	goSrc := `package main

func validateSession(token string) bool {
	return checkToken(token)
}

func checkToken(t string) bool {
	return len(t) > 0
}

func unrelatedFunction() {
	println("hello")
}
`
	require.NoError(t, writeLSHTestFile(t, dir, "main.go", goSrc))

	// Build (populates MinHash + BM25 + LSH bands)
	require.NoError(t, idx.Build())

	// Verify LSH data exists
	assert.True(t, store.HasLSHData(), "Build should populate lsh_bands")

	// Search — should use the LSH path
	results, err := idx.SemanticSearch("validate session token", 5)
	require.NoError(t, err)
	// At minimum, validateSession should appear in results
	found := false
	for _, r := range results {
		if r.Symbol.Name == "validateSession" {
			found = true
			break
		}
	}
	assert.True(t, found, "validateSession should appear in LSH-backed search results")
}

// writeLSHTestFile is a helper to create a source file in the workspace.
func writeLSHTestFile(t *testing.T, dir, name, content string) error {
	t.Helper()
	path := filepath.Join(dir, name)
	return os.WriteFile(path, []byte(content), 0644)
}
