package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderBlocksCanonicalizes(t *testing.T) {
	pb, err := NewProviderBlocks("k", []json.RawMessage{
		json.RawMessage("{ \"type\": \"thinking\",\n  \"thinking\": \"a < b & c\", \"signature\": \"EqQB+/==\" }"),
	})
	require.NoError(t, err)
	assert.Equal(t, "k", pb.Provider)
	assert.Equal(t, `{"type":"thinking","thinking":"a \u003c b \u0026 c","signature":"EqQB+/=="}`, string(pb.Blocks[0]),
		"compact and HTML-escaped: the form encoding/json writes, so a save/load returns the same bytes")
}

func TestNewProviderBlocksRejectsBadInput(t *testing.T) {
	pb, err := NewProviderBlocks("k", nil)
	assert.NoError(t, err)
	assert.Nil(t, pb, "no blocks: nothing to keep")
	_, err = NewProviderBlocks("", []json.RawMessage{json.RawMessage(`{}`)})
	assert.Error(t, err, "blocks without a provider key could never be replayed")
	_, err = NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"a":`)})
	assert.Error(t, err)
}

// MarshalIndent (sessions, agent checkpoints) indents raw JSON too; loading
// re-canonicalizes, so the blocks come back byte for byte, cycle after cycle
// (2.0 F3, ruling 4; W2's "survive save/resume").
func TestProviderBlocksSurviveMarshalIndent(t *testing.T) {
	pb, err := NewProviderBlocks("anthropic-messages|https://api.anthropic.com|m", []json.RawMessage{
		json.RawMessage(`{"type":"thinking","thinking":"plan:\n  1. read <file> & check","signature":"EqQB+/=="}`),
		json.RawMessage(`{"type":"redacted_thinking","data":"ZW5jcnlwdGVk"}`),
		json.RawMessage(`{"type":"tool_use","id":"c1","name":"read_file","input":{"path":"a.txt"}}`),
	})
	require.NoError(t, err)
	pb.Digest = "d"
	msg := SessionMessage{Role: "assistant", ProviderBlocks: pb}
	for cycle := 0; cycle < 2; cycle++ {
		data, err := json.MarshalIndent(msg, "", "  ")
		require.NoError(t, err)
		assert.Contains(t, string(data), `"provider_blocks"`)
		var out SessionMessage
		require.NoError(t, json.Unmarshal(data, &out))
		require.NotNil(t, out.ProviderBlocks)
		assert.Equal(t, pb.Provider, out.ProviderBlocks.Provider)
		assert.Equal(t, "d", out.ProviderBlocks.Digest)
		require.Len(t, out.ProviderBlocks.Blocks, len(pb.Blocks))
		for i := range pb.Blocks {
			assert.Equal(t, string(pb.Blocks[i]), string(out.ProviderBlocks.Blocks[i]), "cycle %d block %d", cycle, i)
		}
		msg = out
	}
}

func TestSessionMessageWithoutBlocksOmitsTheKey(t *testing.T) {
	data, err := json.Marshal(SessionMessage{Role: "user", Content: "hi"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "provider_blocks")
}

// A damaged provider_blocks value never fails a session load: it decodes to
// the zero value, which never replays and is dropped on the next save.
func TestMalformedProviderBlocksNeverFailALoad(t *testing.T) {
	for _, raw := range []string{
		`{"role":"assistant","content":"x","provider_blocks":{"provider":"k","blocks":5}}`,
		`{"role":"assistant","content":"x","provider_blocks":"nonsense"}`,
	} {
		var m SessionMessage
		require.NoError(t, json.Unmarshal([]byte(raw), &m), raw)
		assert.Equal(t, "x", m.Content)
		require.NotNil(t, m.ProviderBlocks)
		assert.Empty(t, m.ProviderBlocks.Provider, raw)
		assert.Empty(t, m.ProviderBlocks.Blocks, raw)
	}
}
