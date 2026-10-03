package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
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

func TestCapLoadedToolResultsClearsBlocksOfACutMessage(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	msgs := []ChatMessage{AttachProviderBlocks(ChatMessage{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("y", 300*1024)}, pb)}
	got := CapLoadedToolResults(msgs, ctxmgr.DefaultMaxToolResultBytes)
	assert.Nil(t, got[0].ProviderBlocks)
}

// Blocks survive save → JSON file (MarshalIndent) → load, byte for byte;
// a blocks-only reply (W1's compaction turn) is kept.
func TestSessionCodecRoundTripsProviderBlocks(t *testing.T) {
	withCall := mustBlocks(t, keyA, `{"type":"thinking","thinking":"read <a>","signature":"S1"}`, `{"type":"tool_use","id":"c1","name":"read_file","input":{"path":"a"}}`)
	compaction := mustBlocks(t, keyA, `{"type":"compaction","content":"summary"}`)
	chat := []ChatMessage{
		{Role: "user", Content: "read it"},
		AttachProviderBlocks(ChatMessage{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`}}}, withCall),
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "alpha"},
		AttachProviderBlocks(ChatMessage{Role: "assistant"}, compaction),
	}
	data, err := json.MarshalIndent(SessionMessagesFromChat(chat), "", "  ")
	require.NoError(t, err)
	var saved []config.SessionMessage
	require.NoError(t, json.Unmarshal(data, &saved))
	restored := ChatMessagesFromSession(saved)
	require.Len(t, restored, 4, "the blocks-only reply is not dropped as empty")
	for i, want := range map[int]*ProviderBlocks{1: withCall, 3: compaction} {
		got, ok := ReplayBlocks(restored[i], keyA)
		require.True(t, ok, "message %d lost its blocks", i)
		require.Len(t, got, len(want.Blocks))
		for j := range got {
			assert.Equal(t, string(want.Blocks[j]), string(got[j]), "message %d block %d", i, j)
		}
	}
}

// Only blocks that still match their message are saved: a reply whose text
// is mid-typing (or was edited) is saved without them (Review Focus 3).
func TestSessionCodecSavesOnlyCurrentBlocks(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"type":"text","text":"Hello"}`)
	msg := AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "Hello"}, pb)
	typing := msg
	typing.Content = "Hel"
	assert.Nil(t, SessionMessagesFromChat([]ChatMessage{typing})[0].ProviderBlocks)
	assert.NotNil(t, SessionMessagesFromChat([]ChatMessage{msg})[0].ProviderBlocks)
}

// A call whose result was never saved is dropped on load; that edits its
// message, which loses its blocks.
func TestSessionCodecClearsBlocksWhenACallIsDropped(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	chat := []ChatMessage{
		{Role: "user", Content: "go"},
		AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "reading", ToolCalls: []ToolCallInfo{{ID: "c1"}, {ID: "c2"}}}, pb),
		{Role: "tool", ToolCallID: "c1", Content: "one"},
	}
	got := ChatMessagesFromSession(SessionMessagesFromChat(chat))
	require.Len(t, got, 3)
	require.Len(t, got[1].ToolCalls, 1)
	assert.Nil(t, got[1].ProviderBlocks)
}
