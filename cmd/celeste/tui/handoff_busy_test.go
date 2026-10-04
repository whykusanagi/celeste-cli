package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestHandoff runs one tool turn, starts /handoff and returns the
// pending HandoffReadyMsg without delivering it.
func startTestHandoff(t *testing.T, client func(*fakeCompactClient)) (AppModel, tea.Msg) {
	t.Helper()
	m, c := newCompactTestApp(t)
	if client != nil {
		client(c)
	}
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
	var ready tea.Msg
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(HandoffReadyMsg); ok {
			ready = msg
		}
	}
	require.NotNil(t, ready, "/handoff produced no HandoffReadyMsg")
	return m, ready
}

func sentMessages(cmd tea.Cmd) []string {
	var out []string
	for _, msg := range collectMsgs(cmd) {
		if s, ok := msg.(SendMessageMsg); ok {
			out = append(out, s.Content)
		}
	}
	return out
}

// While the handoff notes are being written the status bar says so, not
// Ready (#352).
func TestHandoffShowsInProgressStatus(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	assert.NotEqual(t, "Ready", m.status.text)
	assert.Contains(t, strings.ToLower(m.status.text), "handoff")

	m, _ = step(t, m, ready)
	assert.NotContains(t, strings.ToLower(m.status.text), "in progress")
}

// A message typed during a handoff does not run against the old session:
// the handoff still applies, and the message joins the notes in the new
// session's input (#352).
func TestHandoffHoldsChatInputForNewSession(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	before := len(m.chat.GetLLMMessages())

	m, cmd := step(t, m, SendMessageMsg{Content: "and also the lexer"})
	assert.Nil(t, m.turn, "a turn started on the old session during the handoff")
	assert.Len(t, m.chat.GetLLMMessages(), before, "the old session changed during the handoff")
	assert.Empty(t, sentMessages(cmd))

	m, cmd = step(t, m, ready)
	assert.Empty(t, m.chat.GetLLMMessages(), "the handoff was discarded")
	assert.Equal(t, "handoff notes for go\n\nand also the lexer", m.input.Value())
	assert.Empty(t, sentMessages(cmd), "held chat text must wait in the input, not be sent")
}

// A command typed during a handoff waits and then runs in the new session
// (#352).
func TestHandoffQueuesCommandsUntilNewSession(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	before := len(m.chat.GetLLMMessages())

	m, cmd := step(t, m, SendMessageMsg{Content: "/clear"})
	assert.Len(t, m.chat.GetLLMMessages(), before, "/clear ran against the old session during the handoff")
	assert.Empty(t, sentMessages(cmd))

	m, cmd = step(t, m, ready)
	assert.Equal(t, "handoff notes for go", m.input.Value())
	assert.Equal(t, []string{"/clear"}, sentMessages(cmd), "the queued command runs once the new session is in place")
}

// When the handoff fails the held input runs in the session that is still
// current (#352).
func TestHandoffFailureReleasesHeldInput(t *testing.T) {
	m, ready := startTestHandoff(t, func(c *fakeCompactClient) { c.sumErr = errors.New("boom") })

	m, _ = step(t, m, SendMessageMsg{Content: "/stats"})
	m, _ = step(t, m, SendMessageMsg{Content: "keep going"})
	m, cmd := step(t, m, ready)
	assert.Equal(t, []string{"/stats"}, sentMessages(cmd))
	m, cmd = step(t, m, SendMessageMsg{Content: "/stats"})
	_ = m
	assert.Equal(t, []string{"keep going"}, sentMessages(cmd))
}
