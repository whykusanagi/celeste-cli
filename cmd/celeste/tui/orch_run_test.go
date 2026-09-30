package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOrchClient struct {
	fakeToolLLMClient
	runs []uint64
}

func (f *fakeOrchClient) RunOrchestratorCommand(goal string, run uint64) tea.Cmd {
	f.runs = append(f.runs, run)
	return nil
}

func chatHasText(m AppModel, s string) bool {
	for _, msg := range m.chat.GetMessages() {
		if strings.Contains(msg.Content, s) {
			return true
		}
	}
	return false
}

// After Esc on /orch, the cancelled run's late events are ignored: a
// progress event doesn't restart streaming, and its terminal event doesn't
// clear the next turn's cancel (before, it did, so Ctrl+C could no longer
// cancel that turn) or reach the chat.
func TestOrchestratorEventsOfACancelledRunAreIgnored(t *testing.T) {
	client := &fakeOrchClient{}
	m := NewApp(client)

	m, _ = step(t, m, SendMessageMsg{Content: "/orch say hi"})
	require.Len(t, client.runs, 1)
	run := client.runs[0]
	require.NotZero(t, run)
	oldCancelled := false
	m, _ = step(t, m, StreamStartMsg{Cancel: func() { oldCancelled = true }, Run: run})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, oldCancelled, "Esc did not cancel the /orch run")
	require.False(t, m.turnActive())

	m, _ = step(t, m, OrchestratorEventMsg{Kind: 0, Lane: "code", Text: "late", Run: run})
	assert.False(t, m.turnActive(), "a stale progress event reopened the turn")

	// The next turn.
	m, _ = step(t, m, SendMessageMsg{Content: "hello"})
	require.Len(t, client.sendCalls, 1)
	newCancelled := false
	m, _ = step(t, m, StreamStartMsg{Cancel: func() { newCancelled = true }})

	m, _ = step(t, m, OrchestratorEventMsg{Kind: 8, Text: "context canceled", Run: run})
	assert.NotNil(t, m.cancelFunc, "the stale terminal event cleared the new turn's cancel")
	assert.True(t, m.streaming)
	assert.False(t, chatHasText(m, "context canceled"), "the cancelled run's error reached the chat")

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, newCancelled, "Ctrl+C did not cancel the new turn")
}

// A cancel that arrives after its run was already cancelled (Esc before the
// StreamStartMsg) is called, not stored as the current turn's.
func TestOrchestratorLateStreamStartOfACancelledRun(t *testing.T) {
	client := &fakeOrchClient{}
	m := NewApp(client)
	m, _ = step(t, m, SendMessageMsg{Content: "/orch say hi"})
	run := client.runs[0]
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	cancelled := false
	m, _ = step(t, m, StreamStartMsg{Cancel: func() { cancelled = true }, Run: run})
	assert.True(t, cancelled, "the late cancel of a cancelled run was not called")
	assert.Nil(t, m.cancelFunc)
}

// Events of the current run still apply, and a second /orch gets a new tag.
func TestOrchestratorCurrentRunEventsApply(t *testing.T) {
	client := &fakeOrchClient{}
	m := NewApp(client)
	m, _ = step(t, m, SendMessageMsg{Content: "/orch one"})
	m, _ = step(t, m, StreamStartMsg{Cancel: func() {}, Run: client.runs[0]})
	m, _ = step(t, m, OrchestratorEventMsg{Kind: 7, Text: "done", Run: client.runs[0]})
	assert.False(t, m.turnActive())
	_, _ = step(t, m, SendMessageMsg{Content: "/orch two"})
	require.Len(t, client.runs, 2)
	assert.NotEqual(t, client.runs[0], client.runs[1])
}
