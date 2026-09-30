package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func llmRoles(c ChatModel) []string {
	var out []string
	for _, m := range c.GetLLMMessages() {
		out = append(out, m.Role+":"+m.Content)
	}
	return out
}

// New history is appended; UI-only system lines stay where they were.
func TestSyncLLMAppendsAndKeepsSystemLines(t *testing.T) {
	c := NewChatModel().AddSystemMessage("hello").AddUserMessage("go")
	c = c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "go", Metadata: map[string]any{MetaPromptHookDone: true}},
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "alpha"},
	}, false)
	all := c.GetMessages()
	require.Len(t, all, 4)
	assert.Equal(t, "system", all[0].Role)
	assert.Equal(t, true, all[1].Metadata[MetaPromptHookDone], "the loop's metadata replaces the chat's copy")
	assert.Equal(t, []string{"user:go", "assistant:", "tool:alpha"}, llmRoles(c))
}

// Pruning rewrites old tool results in place; the chat follows, position by
// position, and a system line between them keeps its place.
func TestSyncLLMReplacesPrunedToolResultsInPlace(t *testing.T) {
	c := NewChatModel().AddUserMessage("go").
		AddAssistantMessageWithToolCalls("", []ToolCallInfo{{ID: "c1", Name: "read_file"}}).
		AddToolResult("c1", "read_file", "big").
		AddSystemMessage("note").
		AddUserMessage("next")
	c = c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "[pruned]"},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: "done"},
	}, false)
	assert.Equal(t, []string{"user:go", "assistant:", "tool:[pruned]", "user:next", "assistant:done"}, llmRoles(c))
	assert.Equal(t, "note", c.GetMessages()[3].Content)
}

// A reply still being typed keeps its partial text; without keepLive the
// loop's text wins.
func TestSyncLLMKeepsTheLiveTypingBubble(t *testing.T) {
	base := NewChatModel().AddUserMessage("go").AddAssistantMessage("Hel")
	history := []ChatMessage{{Role: "user", Content: "go"}, {Role: "assistant", Content: "Hello"}}
	assert.Equal(t, "assistant:Hel", llmRoles(base.SyncLLM(history, true))[1])
	assert.Equal(t, "assistant:Hello", llmRoles(base.SyncLLM(history, false))[1])
}

// Messages a summary replaced are not sent, so they are not matched either.
func TestSyncLLMSkipsCompactedMessages(t *testing.T) {
	c := NewChatModel().AddUserMessage("old").AddAssistantMessage("old reply")
	c = c.ApplySummary(2, []ChatMessage{{Role: "user", Content: "<summary>"}})
	c = c.AddUserMessage("new")
	c = c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "<summary>", Metadata: map[string]any{"hidden": true}},
		{Role: "user", Content: "new"},
		{Role: "assistant", Content: "answer"},
	}, false)
	assert.Equal(t, []string{"user:<summary>", "user:new", "assistant:answer"}, llmRoles(c))
	assert.Len(t, c.GetMessages(), 5, "the summarized messages stay in the scrollback")
}

func TestAppendLLMKeepsMetadata(t *testing.T) {
	c := NewChatModel().AppendLLM(ChatMessage{Role: "user", Content: "steer", Metadata: map[string]any{MetaPromptHookDone: true}})
	require.Len(t, c.GetMessages(), 1)
	assert.Equal(t, true, c.GetMessages()[0].Metadata[MetaPromptHookDone])
}

// An empty text-only reply the adapter dropped from the loop's input and
// snapshots is dropped from the chat too, so later positions line up.
func TestSyncLLMDropsAnEarlierEmptyReply(t *testing.T) {
	c := NewChatModel().AddUserMessage("u1").AddAssistantMessage("").AddUserMessage("u2")
	checked := []ChatMessage{
		{Role: "user", Content: "u1"},
		{Role: "user", Content: "u2", Metadata: map[string]any{MetaPromptHookDone: true}},
	}
	c = c.SyncLLM(checked, false) // the prompts-checked snapshot
	assert.Equal(t, []string{"user:u1", "user:u2"}, llmRoles(c))
	c = c.SyncLLM(append(checked, ChatMessage{Role: "assistant", Content: "ok"}), false)
	assert.Equal(t, []string{"user:u1", "user:u2", "assistant:ok"}, llmRoles(c))
}

// The last LLM message is the live typing bubble, which starts empty: it is
// a position, not a dropped reply.
func TestSyncLLMKeepsAnEmptyLiveBubble(t *testing.T) {
	c := NewChatModel().AddUserMessage("go").AddAssistantMessage("")
	history := []ChatMessage{{Role: "user", Content: "go"}, {Role: "assistant", Content: "Hello"}}
	assert.Equal(t, []string{"user:go", "assistant:"}, llmRoles(c.SyncLLM(history, true)))
	assert.Equal(t, []string{"user:go", "assistant:Hello"}, llmRoles(c.SyncLLM(history, false)))
}
