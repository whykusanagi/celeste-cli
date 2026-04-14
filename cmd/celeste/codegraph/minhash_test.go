package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMinHasher(t *testing.T) {
	mh := NewMinHasher(128)
	require.NotNil(t, mh)
	assert.Equal(t, 128, mh.numHashes)
}

func TestMinHasher_Signature(t *testing.T) {
	mh := NewMinHasher(128)

	shingles := []string{"validate", "session", "auth", "token", "user"}
	sig := mh.Signature(shingles)

	assert.Len(t, sig, 128)

	// All values should be non-zero (extremely unlikely to be zero)
	nonZero := 0
	for _, v := range sig {
		if v != 0 {
			nonZero++
		}
	}
	assert.Greater(t, nonZero, 100)
}

func TestMinHasher_Signature_Deterministic(t *testing.T) {
	mh := NewMinHasher(128)
	shingles := []string{"hello", "world"}

	sig1 := mh.Signature(shingles)
	sig2 := mh.Signature(shingles)

	assert.Equal(t, sig1, sig2, "same input should produce same signature")
}

func TestMinHasher_Signature_Empty(t *testing.T) {
	mh := NewMinHasher(128)
	sig := mh.Signature(nil)
	assert.Len(t, sig, 128)
}

func TestMinHasher_SimilarSets(t *testing.T) {
	mh := NewMinHasher(128)

	// Two very similar sets
	set1 := []string{"validate", "session", "auth", "token", "user", "check"}
	set2 := []string{"validate", "session", "auth", "token", "user", "verify"}

	sig1 := mh.Signature(set1)
	sig2 := mh.Signature(set2)

	similarity := JaccardSimilarity(sig1, sig2)
	assert.Greater(t, similarity, 0.5, "similar sets should have high similarity")
}

func TestMinHasher_DissimilarSets(t *testing.T) {
	mh := NewMinHasher(128)

	// Two very different sets
	set1 := []string{"validate", "session", "auth", "token", "user"}
	set2 := []string{"render", "html", "template", "css", "layout"}

	sig1 := mh.Signature(set1)
	sig2 := mh.Signature(set2)

	similarity := JaccardSimilarity(sig1, sig2)
	assert.Less(t, similarity, 0.3, "dissimilar sets should have low similarity")
}

func TestJaccardSimilarity_Identical(t *testing.T) {
	sig := MinHashSignature{1, 2, 3, 4, 5}
	assert.Equal(t, 1.0, JaccardSimilarity(sig, sig))
}

func TestJaccardSimilarity_Empty(t *testing.T) {
	assert.Equal(t, 0.0, JaccardSimilarity(nil, nil))
}

func TestJaccardSimilarity_DifferentLengths(t *testing.T) {
	sig1 := MinHashSignature{1, 2, 3}
	sig2 := MinHashSignature{1, 2, 3, 4, 5}
	// Should use the shorter length
	similarity := JaccardSimilarity(sig1, sig2)
	assert.Greater(t, similarity, 0.0)
}

func TestNewMinHasher_GeneratesUnlikelyDuplicateSeeds(t *testing.T) {
	// crypto/rand should produce 128 distinct uint64s with astronomical
	// probability. This is mostly a smoke test — catches a regression
	// where someone might wire up a bad RNG.
	mh := NewMinHasher(128)
	seen := make(map[uint64]bool)
	for _, s := range mh.seeds {
		if seen[s] {
			t.Errorf("duplicate seed found: %x", s)
		}
		seen[s] = true
	}
	assert.Len(t, seen, 128)
}

func TestMinHasher_SeedsRoundTrip(t *testing.T) {
	// Build a hasher, serialize its seeds, reconstruct a new hasher from
	// those seeds, and verify that BOTH hashers produce byte-identical
	// signatures on the same input. This is the core of the v1.9.0 seed
	// persistence story.
	original := NewMinHasher(128)
	seeds := original.Seeds()

	blob := SeedsToBytes(seeds)
	assert.Len(t, blob, 128*8, "each uint64 serializes to 8 bytes")

	restored, err := BytesToSeeds(blob)
	require.NoError(t, err)
	assert.Equal(t, seeds, restored, "serialize→deserialize must round-trip exactly")

	// The restored MinHasher must produce the same signatures as the
	// original — that's what makes stored signatures comparable across
	// process invocations.
	restoredHasher := NewMinHasherFromSeeds(restored)

	shingles := []string{"database", "connection", "pool", "query",
		"mysqlquery", "tempodataquery", "flightsqlquery"}
	origSig := original.Signature(shingles)
	restoredSig := restoredHasher.Signature(shingles)
	assert.Equal(t, origSig, restoredSig, "restored hasher must produce identical signatures")
}

func TestMinHasher_DifferentSeedsDifferentSignatures(t *testing.T) {
	// Two MinHashers with different seeds should produce different
	// signatures on the same input. This is the "before" condition
	// that the persistence fix addresses: without persistence, every
	// process gets fresh random seeds and signatures don't match.
	h1 := NewMinHasher(128)
	h2 := NewMinHasher(128)

	shingles := []string{"database", "connection", "pool"}
	sig1 := h1.Signature(shingles)
	sig2 := h2.Signature(shingles)

	// With 128 random seeds it is essentially impossible for these to
	// collide — require at least 100 positions to differ.
	differences := 0
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			differences++
		}
	}
	assert.Greater(t, differences, 100,
		"two independently-seeded MinHashers must produce mostly-different signatures")
}

func TestBytesToSeeds_RejectsBadLength(t *testing.T) {
	// Length not a multiple of 8 → error.
	_, err := BytesToSeeds([]byte{0, 1, 2})
	require.Error(t, err)

	// Empty input is valid (zero seeds).
	empty, err := BytesToSeeds(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Valid 16-byte input → 2 seeds.
	twoSeeds := SeedsToBytes([]uint64{0xAABBCCDD11223344, 0xDEADBEEFCAFEBABE})
	restored, err := BytesToSeeds(twoSeeds)
	require.NoError(t, err)
	assert.Equal(t, []uint64{0xAABBCCDD11223344, 0xDEADBEEFCAFEBABE}, restored)
}
