package codegraph

import (
	"math"
	"testing"
)

func TestBM25IdfMatchesSmoothedFormula(t *testing.T) {
	for _, tc := range []struct{ df, docs int }{{1, 1000}, {500, 1000}, {1000, 1000}, {1, 1}} {
		want := math.Log((float64(tc.docs-tc.df)+0.5)/(float64(tc.df)+0.5) + 1)
		if got := bm25Idf(tc.df, tc.docs); math.Abs(got-want) > 1e-12 {
			t.Errorf("bm25Idf(%d, %d) = %.12f, want %.12f", tc.df, tc.docs, got, want)
		}
	}
}

func TestBM25IdfRejectsInvalidCorpusFrequency(t *testing.T) {
	for _, tc := range []struct{ df, docs int }{{-1, 10}, {0, 10}, {1, 0}, {11, 10}} {
		if got := bm25Idf(tc.df, tc.docs); got != 0 {
			t.Errorf("bm25Idf(%d, %d) = %v, want 0 for invalid corpus", tc.df, tc.docs, got)
		}
	}
}
