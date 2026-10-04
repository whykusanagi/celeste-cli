package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

// toolTurns fills the chat with n turns, each a prompt, one tool call and
// a reply, with increasing timestamps the way a real session records them.
func toolTurns(m AppModel, n int) AppModel {
	base := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	at := func(turn, step int) time.Time { return base.Add(time.Duration(turn*10+step) * time.Second) }
	for i := 0; i < n; i++ {
		m.chat = m.chat.AddUserMessage(fmt.Sprintf("question %d", i))
		m.chat.messages[len(m.chat.messages)-1].Timestamp = at(i, 0)
		id := fmt.Sprint("call-", i)
		m.chat = m.chat.AddFunctionCall(FunctionCall{ID: id, Name: fmt.Sprintf("read_file_%d", i),
			Arguments: map[string]any{"path": "a/b.go"}, Status: "executing", Timestamp: at(i, 1)})
		m.chat = m.chat.UpdateFunctionResult(id, "", "package main // a result long enough to wrap at eighty columns for sure")
		m.chat = m.chat.AddAssistantMessage(fmt.Sprintf("answer %d done", i))
		m.chat.messages[len(m.chat.messages)-1].Timestamp = at(i, 2)
	}
	m.chat.updateContent()
	m.chat.viewport.GotoBottom()
	return m
}

func pressCtrlK(m AppModel) AppModel {
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	return u.(AppModel)
}

// #351: Ctrl+K keeps a chat that follows the conversation at the bottom,
// with the latest reply and the latest tool log on screen, at both sizes.
func TestCtrlKKeepsLatestReplyAndToolLogVisible(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := toolTurns(newAuditApp(t, &fakeAgentLLMClient{}, sz.w, sz.h), 8)

			m = pressCtrlK(m)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.Contains(t, frame, "answer 7 done", "latest reply visible after Ctrl+K")
			assert.Contains(t, frame, "read_file_7", "latest tool log visible after Ctrl+K")
			assert.True(t, m.chat.viewport.AtBottom())
			// The tool log sits in the turn it belongs to, before its reply.
			assert.Less(t, strings.Index(frame, "read_file_7"), strings.Index(frame, "answer 7 done"))

			m = pressCtrlK(m)
			frame = auditView(m)
			assert.Contains(t, frame, "answer 7 done", "latest reply visible after hiding the log")
			assert.NotContains(t, frame, "read_file_7")
			assert.True(t, m.chat.viewport.AtBottom())
		})
	}
}

// A user who scrolled up keeps their place when the log is toggled.
func TestCtrlKKeepsManualScrollPosition(t *testing.T) {
	m := toolTurns(newAuditApp(t, &fakeAgentLLMClient{}, 80, 24), 8)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = u.(AppModel)
	m = pressCtrlK(m)
	assert.Equal(t, 0, m.chat.viewport.YOffset)
	assert.Contains(t, auditView(m), "question 0")
}
