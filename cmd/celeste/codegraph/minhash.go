package codegraph

import (
	"hash/maphash"
	"math"
)

// DefaultNumHashes is the number of hash functions used for MinHash signatures.
// 128 provides good accuracy with sub-10ms query time for 50k symbols.
const DefaultNumHashes = 128

// MinHasher computes MinHash signatures for sets of shingles.
// Uses hash/maphash with different seeds to simulate N independent hash functions.
type MinHasher struct {
	numHashes int
	seeds     []maphash.Seed
}

// NewMinHasher creates a MinHasher with the specified number of hash functions.
func NewMinHasher(numHashes int) *MinHasher {
	seeds := make([]maphash.Seed, numHashes)
	for i := range seeds {
		seeds[i] = maphash.MakeSeed()
	}
	return &MinHasher{
		numHashes: numHashes,
		seeds:     seeds,
	}
}

// Signature computes the MinHash signature for a set of shingles.
// Each element of the returned slice is the minimum hash value across
// all shingles for that hash function.
func (m *MinHasher) Signature(shingles []string) MinHashSignature {
	sig := make(MinHashSignature, m.numHashes)

	// Initialize all slots to max uint64
	for i := range sig {
		sig[i] = math.MaxUint64
	}

	// For each shingle, compute N hashes and keep the minimum
	for _, shingle := range shingles {
		for i, seed := range m.seeds {
			var h maphash.Hash
			h.SetSeed(seed)
			h.WriteString(shingle)
			val := h.Sum64()
			if val < sig[i] {
				sig[i] = val
			}
		}
	}

	return sig
}

// JaccardSimilarity estimates the Jaccard similarity between two MinHash
// signatures. Returns a value between 0.0 (completely different) and
// 1.0 (identical sets).
func JaccardSimilarity(a, b MinHashSignature) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Use the shorter length
	n := len(a)
	if len(b) < n {
		n = len(b)
	}

	matches := 0
	for i := 0; i < n; i++ {
		if a[i] == b[i] {
			matches++
		}
	}

	return float64(matches) / float64(n)
}
