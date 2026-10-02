package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prompt(s string) ChatMessage { return ChatMessage{Role: "user", Content: s} }

func TestRewindTarget(t *testing.T) {
	msgs := []ChatMessage{
		prompt("first"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "c1"},
		{Role: "assistant", Content: "done"},
		{Role: "system", Content: "a UI line"},
		prompt("second"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c2", Name: "read_file"}, {ID: "c3", Name: "patch_file"}}},
		{Role: "tool", ToolCallID: "c2"},
		{Role: "tool", ToolCallID: "c3"},
	}
	idx, err := rewindTarget(msgs, 1)
	if err != nil || msgs[idx].Content != "second" {
		t.Fatalf("n=1: %d %v", idx, err)
	}
	if ids := callIDsAfter(msgs, idx); len(ids) != 2 || ids[0] != "c2" || ids[1] != "c3" {
		t.Fatalf("ids = %v", ids)
	}
	idx, _ = rewindTarget(msgs, 5)
	if msgs[idx].Content != "first" {
		t.Fatal("n larger than the prompts rewinds to the first")
	}
	if ids := callIDsAfter(msgs, idx); len(ids) != 3 || ids[0] != "c1" {
		t.Fatalf("ids from the first prompt = %v", ids)
	}
	if _, err := rewindTarget([]ChatMessage{{Role: "system", Content: "hi"}}, 1); err == nil {
		t.Fatal("no prompts: nothing to rewind")
	}
}

// Hidden directives and tool results are not prompts.
func TestRewindTargetSkipsNonPrompts(t *testing.T) {
	msgs := []ChatMessage{
		prompt("real"),
		{Role: "user", Content: "identity directive", Metadata: map[string]any{"hidden": true}},
		{Role: "user", Content: "tool output", ToolCallID: "x"},
	}
	idx, err := rewindTarget(msgs, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, idx)
}

// Review Focus 2.
func TestRewindRefusesAcrossACompaction(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "old", Metadata: map[string]any{"compacted": true}},
		{Role: "user", Content: "<compacted-context>\ns\n</compacted-context>", Metadata: map[string]any{"hidden": true}},
		prompt("new"),
	}
	if _, err := rewindTarget(msgs, 2); err == nil {
		t.Fatal("a rewind past a compaction must be refused")
	}
	if idx, err := rewindTarget(msgs, 1); err != nil || idx != 2 {
		t.Fatalf("rewinding the prompt after the summary: %d %v", idx, err)
	}
}

type fakeRewindClient struct {
	fakeCheckpointClient
	rewound  [][]string
	restored []string
	err      error
}

func (f *fakeRewindClient) RewindTo(ids []string) ([]string, error) {
	f.rewound = append(f.rewound, ids)
	return f.restored, f.err
}

func rewindApp(client LLMClient) AppModel {
	m := NewApp(client)
	m.chat = m.chat.RestoreMessages([]ChatMessage{
		prompt("write the files"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "w1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "w1", Content: "ok"},
		{Role: "assistant", Content: "written"},
		prompt("now read them"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "r1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "r1", Content: "x"},
		{Role: "assistant", Content: "read"},
	})
	return m
}

func TestRewindCommandTruncatesAndRefillsTheInput(t *testing.T) {
	client := &fakeRewindClient{restored: []string{"a.go", "b.go"}}
	m := sendCommand(rewindApp(client), "/rewind 2")
	require.Equal(t, [][]string{{"w1", "r1"}}, client.rewound)
	assert.Equal(t, "write the files", m.input.Value())
	for _, msg := range m.chat.GetMessages() {
		assert.NotEqual(t, "written", msg.Content, "the rewound turns must be gone")
	}
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Rewound 2 prompt(s); restored 2 file change(s): a.go, b.go."))
}

func TestRewindCommandWithoutFileChanges(t *testing.T) {
	client := &fakeRewindClient{}
	m := sendCommand(rewindApp(client), "/rewind")
	require.Equal(t, [][]string{{"r1"}}, client.rewound)
	assert.Equal(t, "now read them", m.input.Value())
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Rewound 1 prompt(s); no files were changed by those turns."))
	llm := m.chat.GetLLMMessages()
	assert.Equal(t, "written", llm[len(llm)-1].Content)
}

func TestRewindCommandKeepsTheChatWhenRestoringFails(t *testing.T) {
	client := &fakeRewindClient{err: errors.New("disk full")}
	m := sendCommand(rewindApp(client), "/rewind")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Rewind: restoring files failed: disk full"))
	llm := m.chat.GetLLMMessages()
	assert.Equal(t, "read", llm[len(llm)-1].Content, "a failed restore must not truncate the chat")
	assert.Equal(t, "", m.input.Value())
}

func TestRewindCommandRefusesBadArguments(t *testing.T) {
	client := &fakeRewindClient{}
	m := sendCommand(rewindApp(client), "/rewind two")
	assert.Empty(t, client.rewound)
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Usage: /rewind [n]"))
}

// /rewind and /fork autocomplete like the other commands.
func TestRewindAndForkAreKnownCommands(t *testing.T) {
	for _, name := range []string{"rewind", "fork"} {
		assert.Contains(t, knownCommands, name)
	}
}

// A subagent's writes carry its own call IDs: the rewind line says to
// check /diff.
func TestRewindCommandWarnsAboutSubagents(t *testing.T) {
	client := &fakeRewindClient{}
	m := NewApp(client)
	m.chat = m.chat.RestoreMessages([]ChatMessage{
		prompt("delegate"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "s1", Name: "spawn_agent"}}},
		{Role: "tool", ToolCallID: "s1", Content: "done"},
	})
	m = sendCommand(m, "/rewind")
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Subagents ran in those turns; check /diff"))
}
