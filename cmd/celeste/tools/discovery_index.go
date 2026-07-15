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

// RankDocs ranks free-text documents by BM25 against query and returns their
// indices, best match first. An empty query returns every index in original
// order (no filtering); a non-empty query returns only documents that score
// above zero. Shares the tokenizer + BM25 parameters with ToolIndex, so the
// skills browser ranks with the same relevance as find_tools.
func RankDocs(docs []string, query string) []int {
	if strings.TrimSpace(query) == "" {
		out := make([]int, len(docs))
		for i := range docs {
			out[i] = i
		}
		return out
	}

	df := make(map[string]int)
	terms := make([]map[string]int, len(docs))
	lengths := make([]int, len(docs))
	total := 0
	for i, doc := range docs {
		tf := make(map[string]int)
		toks := tokenize(doc)
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			df[t]++
		}
		terms[i] = tf
		lengths[i] = len(toks)
		total += len(toks)
	}
	avgLen := 0.0
	if len(docs) > 0 {
		avgLen = float64(total) / float64(len(docs))
	}
	n := float64(len(docs))
	qterms := tokenize(query)

	type scored struct {
		idx   int
		score float64
	}
	var results []scored
	for i := range docs {
		var score float64
		for _, qt := range qterms {
			tf, ok := terms[i][qt]
			if !ok {
				continue
			}
			d := float64(df[qt])
			idf := math.Log(1 + (n-d+0.5)/(d+0.5))
			denom := float64(tf) + bm25K1*(1-bm25B+bm25B*float64(lengths[i])/avgLen)
			score += idf * (float64(tf) * (bm25K1 + 1)) / denom
		}
		if score > 0 {
			results = append(results, scored{i, score})
		}
	}
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].score != results[b].score {
			return results[a].score > results[b].score
		}
		return results[a].idx < results[b].idx
	})
	out := make([]int, len(results))
	for i, r := range results {
		out[i] = r.idx
	}
	return out
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
