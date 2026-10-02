package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stream rule's interrupt drops the streamed part of the reply; the
// reminder joins hidden; the re-run's reply is the one the chat keeps
// (2.0 W3).
func TestRuleInterruptDropsThePartialReply(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "make a voice line"})
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: "Audio saved: /tmp/x.mp3", IsFirst: true}})
	m, _ = feed(t, m, RuleInterruptMsg{})
	reminder := ChatMessage{Role: "user", Content: "<system-reminder>\nno audio\n</system-reminder>", Metadata: map[string]any{"hidden": true, MetaPromptHookDone: true}}
	m, _ = feed(t, m, RuleReminderMsg{Source: "rule:unbacked-audio-claim", Message: reminder})

	llm := m.chat.GetLLMMessages()
	require.Len(t, llm, 2, "user prompt then the hidden reminder: %+v", llm)
	assert.Equal(t, "make a voice line", llm[0].Content)
	assert.Equal(t, reminder.Content, llm[1].Content)
	assert.Empty(t, m.typingContent)
	for _, msg := range llm {
		assert.NotContains(t, msg.Content, "Audio saved:", "the interrupted reply is still in the chat")
	}
	assert.Contains(t, lastSystemLine(m), "stream rule stopped the reply")
	assert.NotContains(t, m.chat.View(), "no audio", "a reminder is hidden")
}

// With nothing streamed yet (a tool-arguments rule), the previous reply is
// left alone.
func TestRuleInterruptWithoutStreamedTextKeepsEarlierReplies(t *testing.T) {
	m, _ := newQueueTestApp()
	m.chat = m.chat.AddAssistantMessage("an earlier answer")
	m, _ = step(t, m, SendMessageMsg{Content: "clean the build"})
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, RuleInterruptMsg{})
	found := false
	for _, msg := range m.chat.GetLLMMessages() {
		found = found || msg.Content == "an earlier answer"
	}
	assert.True(t, found, "an interrupt with no live bubble erased an earlier reply")
}

func lastSystemLine(m AppModel) string {
	msgs := m.chat.messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "system" {
			return strings.ToLower(msgs[i].Content)
		}
	}
	return ""
}

// After Esc, a rule event still in flight keeps the chat aligned with the
// loop: the reminder joins the LLM history (the loop's history has it), and
// no retry line is shown for a turn that is ending (review M5).
func TestRuleEventsAfterEsc(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "make a voice line"})
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, m.interrupted)
	m, _ = feed(t, m, RuleInterruptMsg{})
	reminder := ChatMessage{Role: "user", Content: "<system-reminder>\nno audio\n</system-reminder>", Metadata: map[string]any{"hidden": true, MetaPromptHookDone: true}}
	m, _ = feed(t, m, RuleReminderMsg{Source: "rule:unbacked-audio-claim", Message: reminder})
	llm := m.chat.GetLLMMessages()
	require.NotEmpty(t, llm)
	assert.Equal(t, reminder.Content, llm[len(llm)-1].Content)
	assert.NotContains(t, lastSystemLine(m), "stream rule stopped the reply")
}
