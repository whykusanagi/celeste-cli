// Structural rerank layer.
//
// After the Jaccard + BM25 fused ranking produces a preliminary order,
// this layer applies a scalar rescore that incorporates features the
// fusion doesn't see: how many query tokens actually matched the
// symbol's shingle set, how well-connected the symbol is in the call
// graph, and what KIND of symbol it is. The goal is to bubble the
// "obviously relevant" results above close-score ties where the fused
// order alone is ambiguous.
//
// Pure Go, zero dependencies, zero cloud calls. All signals come from
// fields already present on SearchResult, so this layer has no effect
// on indexing latency and only a tiny reorder cost at query time.
//
// The Reranker interface below is a deliberate seam: the current
// StructuralReranker is hand-tuned feature engineering, but a future
// EmbeddingReranker (local llama.cpp bridge, xAI embeddings endpoint,
// ONNX sentence-transformers, ...) can drop in at the same call site
// without touching the search pipeline.
package codegraph

import (
	"math"
	"sort"
)

// Reranker rescores a set of SearchResult candidates using features
// beyond the baseline Jaccard + BM25 fusion. Implementations MUST be
// pure — no network, no file I/O beyond what was passed in. Callers
// are responsible for cloning the input if they need the original
// order preserved; Rerank is allowed to mutate the slice in place.
//
// queryTokenCount is the number of distinct query shingles that made
// it through the stop-word filter. Rerankers that compute a
// matched-token ratio need this for normalization.
type Reranker interface {
	Rerank(results []SearchResult, queryTokenCount int) []SearchResult
}

// StructuralReranker is the default Reranker shipped in v1.9.0. It
// scores each candidate using a weighted combination of features that
// the RRF fusion can't see:
//
//   - MatchedTokenRatio: fraction of query tokens that appear in the
//     symbol's filtered shingle set. A symbol matching 4/4 query
//     tokens should rank above one matching 1/4 even if the Jaccard
//     estimator happens to put them at similar percentiles.
//
//   - EdgeDensity: log-normalized edge count. Well-connected symbols
//     are more likely to be the "real" implementation of a feature
//     than zero-edge stub interfaces. Capped logarithmically so that
//     a symbol with 200 edges doesn't dwarf one with 20.
//
//   - KindWeight: function and method symbols get a small boost over
//     type/interface declarations for implementation-hunting queries.
//     Tuned on the SPEC §5.1 benchmark queries which all target
//     "find me the code that does X" rather than "find me the type
//     definition for X".
//
//   - ZeroEdgePenalty: a symbol with zero edges and kind in
//     {function, method} is either dead code or a parser limitation.
//     Push it below other candidates that actually have connectivity.
//
// All features are normalized to [0,1]-ish ranges before the weighted
// sum. Weights are exposed on the struct so callers can A/B tune
// without recompiling; the zero value uses sensible defaults picked
// by hand-inspection on the Task 23 content-control benchmark.
type StructuralReranker struct {
	// MatchedTokenWeight scales the matched-token-ratio contribution.
	// Default 1.0 — a full-match symbol gets +1.0 added to its base
	// score, which is significant relative to the typical Jaccard
	// range of 0.1-0.2 but doesn't trivially override BM25.
	MatchedTokenWeight float64

	// EdgeDensityWeight scales the log-normalized edge count
	// contribution. Default 0.3 — mild boost; edge count alone
	// shouldn't overwhelm real textual relevance.
	EdgeDensityWeight float64

	// KindBoostFunction is the additive weight for function / method
	// symbols. Default 0.15 — small but enough to break ties in favor
	// of actual implementations over type aliases.
	KindBoostFunction float64

	// ZeroEdgePenalty is the additive weight (usually negative) for
	// function/method symbols with zero edges. Default -0.25 — pushes
	// likely-dead-code below real matches without entirely removing it.
	ZeroEdgePenalty float64
}

// NewStructuralReranker returns a reranker with the default weights
// picked by hand-inspection on the content-control benchmark. Callers
// that want to experiment can construct StructuralReranker{} directly
// with custom weights instead.
func NewStructuralReranker() *StructuralReranker {
	return &StructuralReranker{
		MatchedTokenWeight: 1.0,
		EdgeDensityWeight:  0.3,
		KindBoostFunction:  0.15,
		ZeroEdgePenalty:    -0.25,
	}
}

// Rerank applies the structural rescore to results and returns a new
// ordering. The original Similarity / BM25Score fields on each
// SearchResult are preserved; only the slice order changes. Callers
// can audit the rerank by comparing the old order to the new one.
//
// Ties (exact equal structural scores) are broken by the incoming
// order so the rerank is stable relative to the fused ranking. This
// matters because the fused ranking already encodes meaningful
// signal — we're enhancing it, not replacing it, and ties should
// fall back to "trust the upstream signal".
func (r *StructuralReranker) Rerank(results []SearchResult, queryTokenCount int) []SearchResult {
	if len(results) < 2 {
		return results
	}
	if queryTokenCount <= 0 {
		queryTokenCount = 1
	}

	// Precompute the max edge count for log normalization. Using the
	// max from this candidate set (not a global) keeps the feature
	// comparable within a single query — an edge count of 20 means
	// something different in a small library than in grafana.
	maxEdges := 0
	for _, c := range results {
		if c.EdgeCount > maxEdges {
			maxEdges = c.EdgeCount
		}
	}

	type scored struct {
		result  SearchResult
		score   float64
		origIdx int
	}
	scoredList := make([]scored, len(results))
	for i, c := range results {
		scoredList[i] = scored{
			result:  c,
			score:   r.featureScore(c, queryTokenCount, maxEdges),
			origIdx: i,
		}
	}

	// Add a tiny base signal from the fused position so exact feature
	// ties fall back to the upstream fused ordering instead of random
	// sort chance. Kept small (max 0.05 delta across the whole set)
	// because it's a tiebreaker, not a dominator — structural features
	// with a real signal (like a 0.15 kind boost or 0.25 zero-edge
	// penalty) should still reorder confidently above the ramp.
	n := float64(len(scoredList))
	for i := range scoredList {
		fusedBase := 0.05 * (1.0 - (float64(scoredList[i].origIdx) / n))
		scoredList[i].score += fusedBase
	}

	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].score != scoredList[j].score {
			return scoredList[i].score > scoredList[j].score
		}
		return scoredList[i].origIdx < scoredList[j].origIdx
	})

	out := make([]SearchResult, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.result
	}
	return out
}

// featureScore computes the weighted structural score for one
// candidate. Split out as a method so tests can probe individual
// feature contributions without going through Rerank.
func (r *StructuralReranker) featureScore(c SearchResult, queryTokenCount int, maxEdges int) float64 {
	var score float64

	// Matched-token ratio. Clamped to [0, 1] so a symbol with more
	// matched tokens than we have query tokens (shouldn't happen,
	// but defensive) doesn't overshoot.
	if queryTokenCount > 0 {
		ratio := float64(len(c.MatchedTokens)) / float64(queryTokenCount)
		if ratio > 1 {
			ratio = 1
		}
		score += r.MatchedTokenWeight * ratio
	}

	// Edge density: log-normalized count. log(1+x)/log(1+maxEdges)
	// keeps the contribution bounded in [0,1] regardless of maxEdges.
	if maxEdges > 0 {
		edgeNorm := math.Log1p(float64(c.EdgeCount)) / math.Log1p(float64(maxEdges))
		score += r.EdgeDensityWeight * edgeNorm
	}

	// Kind boost. Functions and methods are what users hunt for when
	// searching for "the code that does X". Types/interfaces get no
	// boost; consts/vars get nothing either.
	switch c.Symbol.Kind {
	case SymbolFunction, SymbolMethod:
		score += r.KindBoostFunction
	}

	// Zero-edge penalty for function/method symbols. Dead code or
	// parser-missed callers either way — push it below real matches.
	if c.EdgeCount == 0 {
		switch c.Symbol.Kind {
		case SymbolFunction, SymbolMethod:
			score += r.ZeroEdgePenalty
		}
	}

	return score
}
