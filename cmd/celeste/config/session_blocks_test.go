package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blocksSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return NewSessionManager()
}

func TestSessionManagerRoundTripsProviderBlocks(t *testing.T) {
	m := blocksSessionManager(t)
	pb, err := NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"type":"thinking","thinking":"x < y","signature":"S"}`)})
	require.NoError(t, err)
	pb.Digest = "d"
	s := m.NewSession()
	s.Name = "blocks"
	s.Messages = []SessionMessage{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "yo", ProviderBlocks: pb}}
	require.NoError(t, m.Save(s))
	for cycle := 0; cycle < 2; cycle++ {
		got, err := m.Load(s.ID)
		require.NoError(t, err)
		require.Len(t, got.Messages, 2)
		assert.Nil(t, got.Messages[0].ProviderBlocks)
		require.NotNil(t, got.Messages[1].ProviderBlocks, "cycle %d", cycle)
		assert.Equal(t, string(pb.Blocks[0]), string(got.Messages[1].ProviderBlocks.Blocks[0]), "cycle %d", cycle)
		require.NoError(t, m.Save(got))
	}
}

// A session written before 2.0 (no provider_blocks anywhere) loads unchanged
// and re-saves without gaining any (spec §6.2).
func TestPre2SessionFileLoadsUnchanged(t *testing.T) {
	m := blocksSessionManager(t)
	const id = "1700000000000000000"
	const legacy = `{
  "id": "1700000000000000000",
  "name": "read a file",
  "created_at": "2026-08-01T10:00:00Z",
  "updated_at": "2026-08-01T10:05:00Z",
  "messages": [
    {"role": "user", "content": "read a.txt", "timestamp": "2026-08-01T10:00:00Z"},
    {"role": "assistant", "content": "", "timestamp": "2026-08-01T10:00:01Z",
     "tool_calls": [{"id": "c1", "name": "read_file", "arguments": "{\"path\":\"a.txt\"}"}]},
    {"role": "tool", "content": "alpha", "timestamp": "2026-08-01T10:00:02Z", "tool_call_id": "c1", "name": "read_file"},
    {"role": "assistant", "content": "it says alpha", "timestamp": "2026-08-01T10:00:03Z"}
  ],
  "model": "fake-model",
  "provider": "openai"
}`
	path := filepath.Join(m.sessionsDir, id+".json")
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o644))
	got, err := m.Load(id)
	require.NoError(t, err)
	require.Len(t, got.Messages, 4)
	for i, msg := range got.Messages {
		assert.Nil(t, msg.ProviderBlocks, "message %d", i)
	}
	assert.Equal(t, `{"path":"a.txt"}`, got.Messages[1].ToolCalls[0].Arguments)
	assert.Equal(t, "it says alpha", got.Messages[3].Content)
	require.NoError(t, m.Save(got))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "provider_blocks")
}
