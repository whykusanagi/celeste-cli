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
