package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Sessions keep tool calls, tool results and the hidden/compacted flags, so
// a resumed session sends the same history (#174).
func TestSessionCodecRoundTripsToolTraffic(t *testing.T) {
	chat := []ChatMessage{
		{Role: "system", Content: "ui only"},
		{Role: "user", Content: "old", Metadata: map[string]any{"compacted": true}},
		{Role: "user", Content: "<compacted-context>s</compacted-context>", Metadata: map[string]any{"hidden": true}},
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`, ThoughtSignature: []byte("sig")}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "contents", Metadata: map[string]any{"image": "big"}},
		{Role: "assistant", Content: "done"},
	}

	saved := SessionMessagesFromChat(chat)
	require.Len(t, saved, 6, "system messages are not saved")
	assert.True(t, saved[0].Compacted)
	assert.True(t, saved[1].Hidden)
	require.Len(t, saved[3].ToolCalls, 1)
	assert.Equal(t, config.SessionToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`, ThoughtSignature: []byte("sig")}, saved[3].ToolCalls[0])
	assert.Equal(t, "c1", saved[4].ToolCallID)

	restored := ChatMessagesFromSession(saved)
	require.Len(t, restored, 6)
	assert.Equal(t, chat[1:4], restored[:3], "flags come back as metadata")
	assert.Equal(t, chat[4].ToolCalls, restored[3].ToolCalls)
	assert.Equal(t, "contents", restored[4].Content)
	assert.Nil(t, restored[4].Metadata, "tool-result attachments are not saved")
}

// A call without its result (saved mid tool loop) or a result without its
// call would be rejected by the provider; both are dropped on restore.
func TestSessionCodecDropsUnpairedToolTraffic(t *testing.T) {
	saved := []config.SessionMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "reading", ToolCalls: []config.SessionToolCall{{ID: "c1"}, {ID: "c2"}}},
		{Role: "tool", ToolCallID: "c1", Content: "one"},
		{Role: "tool", ToolCallID: "stray", Content: "orphan"},
		{Role: "assistant", ToolCalls: []config.SessionToolCall{{ID: "c3"}}},
	}

	got := ChatMessagesFromSession(saved)
	require.Len(t, got, 3)
	assert.Equal(t, "reading", got[1].Content)
	require.Len(t, got[1].ToolCalls, 1)
	assert.Equal(t, "c1", got[1].ToolCalls[0].ID)
	assert.Equal(t, "c1", got[2].ToolCallID)
}
