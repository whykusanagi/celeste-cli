package tui

import (
	"os"
	"strings"
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
		AppendLLM(ChatMessage{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "big"}).
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

// Only the last LLM message is live: an earlier assistant reply takes the
// loop's text even while the latest one is being typed.
func TestSyncLLMKeepsOnlyTheLastReplyLive(t *testing.T) {
	c := NewChatModel().AddUserMessage("a").AddAssistantMessage("first, chat copy").
		AddUserMessage("b").AddAssistantMessage("Sec")
	c = c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "first, loop copy"},
		{Role: "user", Content: "b"},
		{Role: "assistant", Content: "Second"},
	}, true)
	assert.Equal(t, []string{"user:a", "assistant:first, loop copy", "user:b", "assistant:Sec"}, llmRoles(c))
}

// "Last" is the last LLM message: a summarized (compacted) message after it
// is not one. Not produced by today's writers (summaries compact the head),
// but it pins what last means for keepLive and the empty-bubble exemption.
func TestSyncLLMLastSkipsCompactedMessages(t *testing.T) {
	compacted := ChatMessage{Role: "user", Content: "old", Metadata: map[string]any{"compacted": true}}
	history := []ChatMessage{{Role: "user", Content: "go"}, {Role: "assistant", Content: "Hello"}}

	c := NewChatModel().AddUserMessage("go").AddAssistantMessage("Hel").AppendLLM(compacted)
	assert.Equal(t, []string{"user:go", "assistant:Hel"}, llmRoles(c.SyncLLM(history, true)))

	c = NewChatModel().AddUserMessage("go").AddAssistantMessage("").AppendLLM(compacted)
	assert.Equal(t, []string{"user:go", "assistant:"}, llmRoles(c.SyncLLM(history, true)),
		"the empty live bubble is a position, not a dropped reply")
}

// Only an empty text-only assistant reply is dropped. An empty tool result
// is a position: dropping it would move a system line past it.
func TestSyncLLMKeepsAnEmptyToolResult(t *testing.T) {
	c := NewChatModel().AddUserMessage("go").
		AddAssistantMessageWithToolCalls("", []ToolCallInfo{{ID: "c1", Name: "read_file"}}).
		AppendLLM(ChatMessage{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: ""}).
		AddSystemMessage("note").
		AddUserMessage("next")
	c = c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: ""},
		{Role: "user", Content: "next"},
	}, false)
	var roles []string
	for _, msg := range c.GetMessages() {
		roles = append(roles, msg.Role)
	}
	assert.Equal(t, []string{"user", "assistant", "tool", "system", "user"}, roles)
}

// keepLive keeps a live assistant bubble's text only over the loop's
// assistant reply at that position; any other last message takes the
// loop's copy.
func TestSyncLLMKeepLiveOnlyForAnAssistantOverAnAssistant(t *testing.T) {
	cases := []struct {
		name    string
		chat    ChatModel
		history []ChatMessage
		want    []string
	}{
		{
			name: "pruned tool result",
			chat: NewChatModel().AddUserMessage("go").
				AddAssistantMessageWithToolCalls("", []ToolCallInfo{{ID: "c1", Name: "read_file"}}).
				AppendLLM(ChatMessage{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "big"}),
			history: []ChatMessage{
				{Role: "user", Content: "go"},
				{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file"}}},
				{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "[pruned]"},
			},
			want: []string{"user:go", "assistant:", "tool:[pruned]"},
		},
		{
			name:    "user in the chat, assistant in the loop",
			chat:    NewChatModel().AddUserMessage("go").AddUserMessage("steer"),
			history: []ChatMessage{{Role: "user", Content: "go"}, {Role: "assistant", Content: "reply"}},
			want:    []string{"user:go", "assistant:reply"},
		},
		{
			name:    "assistant in the chat, user in the loop",
			chat:    NewChatModel().AddUserMessage("go").AddAssistantMessage("Hel"),
			history: []ChatMessage{{Role: "user", Content: "go"}, {Role: "user", Content: "steer"}},
			want:    []string{"user:go", "user:steer"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, llmRoles(tc.chat.SyncLLM(tc.history, true)))
		})
	}
}

// A snapshot that disagrees with the chat at a position (role, tool call
// ID, or a user prompt's content and timestamp) is logged: the positional
// sync would otherwise hide the drift.
func TestSyncLLMLogsDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, InitLogging())
	path := GetLogPath()

	c := NewChatModel().AddUserMessage("go")
	ts := c.GetLLMMessages()[0].Timestamp
	aligned := []ChatMessage{{Role: "user", Content: "go", Timestamp: ts}, {Role: "assistant", Content: "ok"}}
	c = c.SyncLLM(aligned, false)
	c.SyncLLM([]ChatMessage{
		{Role: "user", Content: "go", Timestamp: ts},
		{Role: "user", Content: "something else"},
	}, false)
	CloseLogging()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var drift []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "history drift") {
			drift = append(drift, line)
		}
	}
	require.Len(t, drift, 1, "only the misaligned snapshot is logged:\n%s", data)
	assert.Contains(t, drift[0], "position 1")
	assert.Contains(t, drift[0], "role assistant")
}

// A summary cut is an edit of the cut messages: they stay in the scrollback,
// marked compacted, without blocks. The kept tail keeps its blocks.
func TestApplySummaryClearsBlocksOfCutMessages(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	c := NewChatModel().AddUserMessage("old")
	c = c.AppendLLM(AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "old reply"}, pb))
	c = c.AddUserMessage("new")
	c = c.AppendLLM(AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "kept reply"}, pb))
	c = c.ApplySummary(2, []ChatMessage{{Role: "user", Content: "<summary>"}})
	all := c.GetMessages()
	require.Len(t, all, 5) // old, old reply (compacted), summary (hidden), new, kept reply
	assert.Equal(t, "old reply", all[1].Content)
	assert.Nil(t, all[1].ProviderBlocks, "a message the summary replaced loses its blocks")
	assert.Equal(t, "kept reply", all[4].Content)
	assert.NotNil(t, all[4].ProviderBlocks, "the kept tail keeps its blocks")
}

func TestReplaceToolResultsClearsBlocks(t *testing.T) {
	pb := mustBlocks(t, keyA, `{"a":1}`)
	c := NewChatModel().AppendLLM(AttachProviderBlocks(ChatMessage{Role: "tool", ToolCallID: "c1", Content: "big"}, pb))
	c = c.ReplaceToolResults(map[string]string{"c1": "[pruned]"})
	assert.Equal(t, "[pruned]", c.GetMessages()[0].Content)
	assert.Nil(t, c.GetMessages()[0].ProviderBlocks)
}
