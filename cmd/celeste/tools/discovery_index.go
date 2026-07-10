package tools

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 parameters (Okapi BM25 defaults).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// tokenize lower-cases and splits on any non-alphanumeric rune.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

type toolDoc struct {
	name   string
	terms  map[string]int // term -> frequency
	length int
}

// ToolIndex is a BM25 index over tool name+description text.
type ToolIndex struct {
	docs   []toolDoc
	df     map[string]int // document frequency per term
	avgLen float64
}

// BuildToolIndex indexes each tool's name+description for BM25 search.
func BuildToolIndex(ts []Tool) *ToolIndex {
	ix := &ToolIndex{df: make(map[string]int)}
	total := 0
	for _, t := range ts {
		terms := tokenize(t.Name() + " " + t.Description())
		tf := make(map[string]int, len(terms))
		for _, term := range terms {
			tf[term]++
		}
		for term := range tf {
			ix.df[term]++
		}
		ix.docs = append(ix.docs, toolDoc{name: t.Name(), terms: tf, length: len(terms)})
		total += len(terms)
	}
	if len(ix.docs) > 0 {
		ix.avgLen = float64(total) / float64(len(ix.docs))
	}
	return ix
}

// Search returns up to topN tool names ranked by BM25 score. Zero-score
// documents are excluded, so a query with no term overlap returns nothing.
func (ix *ToolIndex) Search(query string, topN int) []string {
	qterms := tokenize(query)
	n := float64(len(ix.docs))

	type scored struct {
		name  string
		score float64
	}
	var results []scored

	for _, doc := range ix.docs {
		var score float64
		for _, qt := range qterms {
			tf, ok := doc.terms[qt]
			if !ok {
				continue
			}
			df := float64(ix.df[qt])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			denom := float64(tf) + bm25K1*(1-bm25B+bm25B*float64(doc.length)/ix.avgLen)
			score += idf * (float64(tf) * (bm25K1 + 1)) / denom
		}
		if score > 0 {
			results = append(results, scored{doc.name, score})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].name < results[j].name // stable tiebreak
	})

	if topN > len(results) {
		topN = len(results)
	}
	out := make([]string, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, results[i].name)
	}
	return out
}
