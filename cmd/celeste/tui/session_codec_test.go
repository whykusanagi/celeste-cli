package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
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

// A resumed session's answered prompts are not re-checked by
// UserPromptSubmit; a trailing unanswered prompt still is (2.0 F0).
func TestRestoreMessagesMarksAnsweredPromptsHooked(t *testing.T) {
	in := []ChatMessage{
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "kept"},
	}
	got := NewChatModel().RestoreMessages(in).GetMessages()
	require.Len(t, got, 3)
	done := func(m ChatMessage) bool { v, _ := m.Metadata[MetaPromptHookDone].(bool); return v }
	assert.True(t, done(got[0]), "answered prompt should be marked")
	assert.False(t, done(got[2]), "trailing prompt must stay unchecked")
	assert.Nil(t, in[0].Metadata, "caller's slice must not be modified")
}

// A session written before 2.0 may hold a tool result nothing ever capped;
// with no transport trim (2.0 F3) it is cut once, on load (ruling 9).
func TestSessionCodecCapsOversizedLegacyToolResults(t *testing.T) {
	huge := strings.Repeat("y", 300*1024)
	saved := []config.SessionMessage{
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []config.SessionToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: huge},
	}
	got := ChatMessagesFromSession(saved)
	require.Len(t, got, 3)
	assert.LessOrEqual(t, len(got[2].Content), ctxmgr.DefaultMaxToolResultBytes)
	assert.Contains(t, got[2].Content, "snipped")
	assert.Contains(t, got[2].Content, "no longer available", "the model is told the middle cannot be recalled")
	assert.Equal(t, huge, saved[2].Content, "the saved session itself is not modified")
}

// CapToolResult's own previews are exactly the cap long and stay whole; only
// tool messages are cut; with nothing to cut the input comes back.
func TestCapLoadedToolResultsLeavesCappedPreviewsAlone(t *testing.T) {
	preview := strings.Repeat("z", ctxmgr.DefaultMaxToolResultBytes)
	msgs := []ChatMessage{
		{Role: "tool", ToolCallID: "c1", Content: preview},
		{Role: "user", Content: strings.Repeat("u", 300*1024)},
	}
	got := CapLoadedToolResults(msgs, ctxmgr.DefaultMaxToolResultBytes)
	assert.Equal(t, preview, got[0].Content)
	assert.Len(t, got[1].Content, 300*1024, "only tool results are capped")
	assert.Same(t, &msgs[0], &got[0], "nothing to cut: the input slice comes back")
}
