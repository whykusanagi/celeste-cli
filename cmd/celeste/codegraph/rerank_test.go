package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper: build a synthetic SearchResult with only the fields the
// reranker looks at. ID is sequential so ties break predictably.
func mkResult(id int64, name string, kind SymbolKind, edges int, matched []string) SearchResult {
	return SearchResult{
		Symbol: Symbol{
			ID:   id,
			Name: name,
			Kind: kind,
			File: "test.ts",
		},
		EdgeCount:     edges,
		MatchedTokens: matched,
	}
}

func TestStructuralReranker_MatchedTokenRatioBoostsFullMatch(t *testing.T) {
	// Two candidates in fused order [weakMatch, fullMatch]. The reranker
	// should bubble the full-match symbol above the weak-match one even
	// though the fused order put it second.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "readThing", SymbolFunction, 3, []string{"read"}),
		mkResult(2, "readWriteParseFile", SymbolFunction, 3, []string{"read", "write", "parse", "file"}),
	}
	reranked := r.Rerank(results, 4)
	require.Len(t, reranked, 2)
	assert.Equal(t, int64(2), reranked[0].Symbol.ID, "full-match symbol should rerank to #1")
}

func TestStructuralReranker_ZeroEdgePenaltyOnFunction(t *testing.T) {
	// Two candidates, both match the same number of tokens. One has
	// 10 edges (real implementation), one has 0 (likely dead code or
	// parser-missed). Even though the zero-edge one is first in the
	// fused order, the reranker should swap them.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "deadCode", SymbolFunction, 0, []string{"read", "write"}),
		mkResult(2, "realImpl", SymbolFunction, 10, []string{"read", "write"}),
	}
	reranked := r.Rerank(results, 2)
	assert.Equal(t, int64(2), reranked[0].Symbol.ID, "zero-edge function should be penalized below real-impl")
}

func TestStructuralReranker_PreservesOrderOnTies(t *testing.T) {
	// Three candidates that produce IDENTICAL structural scores (same
	// kind, same edge density, same matched tokens) should fall back to
	// the incoming fused order. This matters because the fused order
	// already encodes meaningful signal — ties should inherit it.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "alpha", SymbolFunction, 5, []string{"x"}),
		mkResult(2, "beta", SymbolFunction, 5, []string{"x"}),
		mkResult(3, "gamma", SymbolFunction, 5, []string{"x"}),
	}
	reranked := r.Rerank(results, 1)
	for i, r := range reranked {
		assert.Equal(t, int64(i+1), r.Symbol.ID, "ties must preserve incoming order")
	}
}

func TestStructuralReranker_KindBoostPrefersFunctionOverInterface(t *testing.T) {
	// Two candidates matching the same tokens with the same edge count.
	// One is a function, one is an interface. The reranker should
	// prefer the function since "find code that does X" queries
	// usually want the implementation, not the type.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "UserSession", SymbolInterface, 2, []string{"user", "session"}),
		mkResult(2, "createUserSession", SymbolFunction, 2, []string{"user", "session"}),
	}
	reranked := r.Rerank(results, 2)
	assert.Equal(t, int64(2), reranked[0].Symbol.ID, "function should beat interface with identical other features")
}

func TestStructuralReranker_EdgeDensityBreaksTieBetweenFunctions(t *testing.T) {
	// Two functions with identical matched tokens. One has many more
	// edges. The well-connected one should rerank higher.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "lightlyUsed", SymbolFunction, 1, []string{"parse"}),
		mkResult(2, "heavilyUsed", SymbolFunction, 50, []string{"parse"}),
	}
	reranked := r.Rerank(results, 1)
	assert.Equal(t, int64(2), reranked[0].Symbol.ID, "high-edge function should beat low-edge function")
}

func TestStructuralReranker_EmptyInputNoOp(t *testing.T) {
	r := NewStructuralReranker()
	assert.Empty(t, r.Rerank(nil, 3))
	assert.Empty(t, r.Rerank([]SearchResult{}, 3))
}

func TestStructuralReranker_SingleResultNoOp(t *testing.T) {
	// With one result there's no reordering possible. The slice should
	// come back unchanged.
	r := NewStructuralReranker()
	results := []SearchResult{mkResult(42, "only", SymbolFunction, 5, []string{"x"})}
	reranked := r.Rerank(results, 1)
	require.Len(t, reranked, 1)
	assert.Equal(t, int64(42), reranked[0].Symbol.ID)
}

func TestStructuralReranker_QueryTokenCountZeroIsSafe(t *testing.T) {
	// Defensive: queryTokenCount=0 shouldn't divide by zero.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "a", SymbolFunction, 5, nil),
		mkResult(2, "b", SymbolFunction, 10, nil),
	}
	reranked := r.Rerank(results, 0)
	require.Len(t, reranked, 2)
	// With no query tokens, matched-token feature is zero. Edge density
	// favors id=2. Should still produce a stable ordering.
	assert.Equal(t, int64(2), reranked[0].Symbol.ID)
}

func TestStructuralReranker_CustomWeights(t *testing.T) {
	// Exposed weights should actually influence the ranking. Crank the
	// kind boost so function beats interface even when the interface
	// has matched tokens the function doesn't.
	r := &StructuralReranker{
		MatchedTokenWeight: 0.1, // small — de-emphasize matched tokens
		EdgeDensityWeight:  0.1,
		KindBoostFunction:  5.0, // huge — dominate everything
		ZeroEdgePenalty:    0,
	}
	results := []SearchResult{
		mkResult(1, "Matches", SymbolInterface, 5, []string{"a", "b", "c", "d"}),
		mkResult(2, "doesX", SymbolFunction, 5, []string{"a"}),
	}
	reranked := r.Rerank(results, 4)
	assert.Equal(t, int64(2), reranked[0].Symbol.ID, "huge function kind boost must override matched-token lead")
}

func TestStructuralReranker_FusedPositionStillContributes(t *testing.T) {
	// The rerank uses a linear-ramp base signal from the fused position
	// so strong fused matches don't lose to weak matches that happen to
	// hit more tokens. Two candidates with near-equal structural scores
	// should still reflect the fused ordering through the base ramp.
	r := NewStructuralReranker()
	results := []SearchResult{
		mkResult(1, "fusedWinner", SymbolFunction, 5, []string{"x"}),
		mkResult(2, "fusedLoser", SymbolFunction, 5, []string{"x"}),
	}
	reranked := r.Rerank(results, 1)
	// Identical features except position — winner should stay first.
	assert.Equal(t, int64(1), reranked[0].Symbol.ID)
}
