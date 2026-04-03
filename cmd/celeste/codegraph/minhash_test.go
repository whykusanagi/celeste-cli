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
