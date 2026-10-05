package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C5: a turn's tool log sits before its reply even when the provider
// answers in milliseconds, so the loop stamps the reply before the chat
// gets to the call's start event. The call carries the loop's start time.
func TestToolLogBeforeFastReply(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			t0 := time.Now().Add(-time.Second) // the loop ran ahead of the chat
			at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
			m, _ = step(t, m, SendMessageMsg{Content: "QUESTION"})
			last := len(m.chat.messages) - 1
			require.Equal(t, "QUESTION", m.chat.messages[last].Content)
			m.chat.messages[last].Timestamp = at(0)
			user := m.chat.messages[last]
			ask := ChatMessage{Role: "assistant", Content: "", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "tool_a", Arguments: `{}`}}, Timestamp: at(1)}
			result := ChatMessage{Role: "tool", ToolCallID: "c1", Name: "tool_a", Content: `{"ok":true}`, Timestamp: at(3)}
			reply := ChatMessage{Role: "assistant", Content: "FASTREPLY", Timestamp: at(4)}

			m, _ = feed(t, m, TurnStartMsg{Turn: 1})
			m, _ = feed(t, m, ToolTurnMsg{Text: ""})
			m, _ = feed(t, m, HistoryMsg{History: []ChatMessage{user, ask}})
			m, _ = feed(t, m, ToolStartMsg{ID: "c1", Name: "tool_a", Started: at(2)})
			m, _ = feed(t, m, ToolResultMsg{ID: "c1", Name: "tool_a", Content: `{"ok":true}`})
			m, _ = feed(t, m, HistoryMsg{History: []ChatMessage{user, ask, result, reply}})
			m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
			m = pressCtrlK(m)

			frame := auditView(m)
			call := strings.Index(frame, "✓ tool_a")
			answer := strings.Index(frame, "FASTREPLY")
			question := strings.Index(frame, "QUESTION")
			require.True(t, call >= 0 && answer >= 0 && question >= 0, frame)
			assert.Less(t, question, call, frame)
			assert.Less(t, call, answer, "the tool log renders before the reply:\n%s", frame)
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}
