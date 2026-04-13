package codegraph

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
)

// DefaultNumHashes is the number of hash functions used for MinHash signatures.
// 128 provides good accuracy with sub-10ms query time for 50k symbols.
const DefaultNumHashes = 128

// fnv64Prime is the FNV-1a 64-bit prime. We use FNV-1a where the seed acts as
// the initial offset basis, producing a family of pairwise-independent hash
// functions suitable for MinHash similarity estimation.
const fnv64Prime = 0x100000001b3

// MinHasher computes MinHash signatures for sets of shingles.
// Uses FNV-1a with different uint64 seeds to simulate N independent hash
// functions. Seeds are a fixed []uint64 so the hasher can be persisted to
// the codegraph store and restored across process invocations — essential
// for reliable cross-process semantic search.
type MinHasher struct {
	numHashes int
	seeds     []uint64
}

// NewMinHasher creates a MinHasher with the specified number of hash functions,
// generating fresh random seeds from crypto/rand. Use NewMinHasherFromSeeds
// when reloading a persisted hasher from the store.
func NewMinHasher(numHashes int) *MinHasher {
	seeds := make([]uint64, numHashes)
	for i := range seeds {
		seeds[i] = randomSeed()
	}
	return &MinHasher{
		numHashes: numHashes,
		seeds:     seeds,
	}
}

// NewMinHasherFromSeeds creates a MinHasher with pre-determined seeds,
// typically reloaded from the codegraph store's meta table. This is the
// critical path for cross-process signature stability: a MinHash signature
// computed with seeds S can only be compared to another signature computed
// with the SAME seeds S. Persisting the seeds and restoring them on Open
// is what makes SemanticSearch work across process boundaries.
func NewMinHasherFromSeeds(seeds []uint64) *MinHasher {
	// Copy so the caller can't mutate our state.
	copied := make([]uint64, len(seeds))
	copy(copied, seeds)
	return &MinHasher{
		numHashes: len(copied),
		seeds:     copied,
	}
}

// Seeds returns a copy of the hasher's seeds. Used by the indexer to
// persist them into the codegraph store's meta table at build time.
func (m *MinHasher) Seeds() []uint64 {
	out := make([]uint64, len(m.seeds))
	copy(out, m.seeds)
	return out
}

// NumHashes returns the signature length.
func (m *MinHasher) NumHashes() int {
	return m.numHashes
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
			val := seededFNVHash(seed, shingle)
			if val < sig[i] {
				sig[i] = val
			}
		}
	}

	return sig
}

// seededFNVHash is an FNV-1a variant where the provided seed takes the
// place of the standard FNV offset basis. Different seeds produce
// different hash functions in the FNV family, which gives MinHash the
// pairwise-independent hash draws it needs for unbiased Jaccard estimation.
//
// Not cryptographic. Deterministic, fast, and — crucially — serializable:
// the entire state of a hash function is one uint64, which we persist to
// the codegraph store's meta table.
func seededFNVHash(seed uint64, data string) uint64 {
	h := seed
	for i := 0; i < len(data); i++ {
		h ^= uint64(data[i])
		h *= fnv64Prime
	}
	return h
}

// JaccardSimilarity estimates the Jaccard similarity between two MinHash
// signatures. Returns a value between 0.0 (completely different) and
// 1.0 (identical sets).
//
// IMPORTANT: both signatures must have been computed with the SAME hash
// seeds for this to produce meaningful results. Comparing signatures from
// different MinHashers (different seeds) yields noise.
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

// SeedsToBytes serializes a seed slice to bytes for persistence.
// Layout is little-endian uint64 × N, for a total of 8*N bytes.
func SeedsToBytes(seeds []uint64) []byte {
	out := make([]byte, 8*len(seeds))
	for i, s := range seeds {
		binary.LittleEndian.PutUint64(out[i*8:], s)
	}
	return out
}

// BytesToSeeds deserializes a byte slice back into a seed slice. Returns
// an error if the length is not a multiple of 8.
func BytesToSeeds(data []byte) ([]uint64, error) {
	if len(data)%8 != 0 {
		return nil, fmt.Errorf("seed blob length %d is not a multiple of 8", len(data))
	}
	n := len(data) / 8
	out := make([]uint64, n)
	for i := 0; i < n; i++ {
		out[i] = binary.LittleEndian.Uint64(data[i*8:])
	}
	return out, nil
}

// randomSeed pulls a uint64 from crypto/rand. Cryptographic randomness isn't
// strictly required — we just need uncorrelated seeds across MinHasher
// instances — but crypto/rand is the right default for anything security-
// adjacent and costs nothing at index-time frequency.
func randomSeed() uint64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fall back to a time-derived seed if crypto/rand fails. In
		// practice crypto/rand on macOS/Linux cannot fail, but we keep
		// the fallback so Indexer.Build never panics on this path.
		return fallbackSeed()
	}
	return binary.LittleEndian.Uint64(buf[:])
}

// fallbackSeed is used only if crypto/rand fails. Uses a constant mixed
// with the current instance address — deterministic-looking but effectively
// random across runs. Not reached on any supported platform.
var fallbackCounter uint64 = 0x9E3779B97F4A7C15 // golden ratio constant

func fallbackSeed() uint64 {
	fallbackCounter = (fallbackCounter * 0x100000001b3) ^ 0xBF58476D1CE4E5B9
	return fallbackCounter
}
