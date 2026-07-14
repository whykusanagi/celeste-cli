package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolIndex_RanksRelevantFirst(t *testing.T) {
	ts := []Tool{
		stubTool{name: "convert_currency", desc: "Convert an amount between fiat currencies using live exchange rates"},
		stubTool{name: "generate_uuid", desc: "Generate a random UUID identifier"},
		stubTool{name: "get_weather", desc: "Look up the current weather for a city"},
	}
	ix := BuildToolIndex(ts)

	got := ix.Search("exchange money between currencies", 2)
	require.NotEmpty(t, got)
	assert.Equal(t, "convert_currency", got[0], "currency tool ranks first for a money query")
	assert.LessOrEqual(t, len(got), 2)
}

func TestToolIndex_NoMatchReturnsEmpty(t *testing.T) {
	ts := []Tool{stubTool{name: "get_weather", desc: "Look up the current weather"}}
	ix := BuildToolIndex(ts)
	assert.Empty(t, ix.Search("quantum chromodynamics blockchain", 5))
}

func TestTokenize(t *testing.T) {
	assert.Equal(t, []string{"convert", "currency", "usd"}, tokenize("Convert_Currency (USD)"))
}

func TestRankDocs_EmptyQueryReturnsAllInOrder(t *testing.T) {
	got := RankDocs([]string{"alpha", "beta", "gamma"}, "")
	assert.Equal(t, []int{0, 1, 2}, got)
}

func TestRankDocs_RanksRelevantAndFiltersOut(t *testing.T) {
	docs := []string{
		"convert_currency convert money between fiat currencies at live rates",
		"generate_uuid generate a random unique identifier",
		"get_weather look up the current weather for a city",
	}
	got := RankDocs(docs, "exchange money currency")
	require.NotEmpty(t, got)
	assert.Equal(t, 0, got[0], "currency doc ranks first")
	assert.NotContains(t, got, 1, "uuid doc has no term overlap and is excluded")
}
