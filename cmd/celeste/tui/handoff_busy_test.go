package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

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

// /agents, which runs even during a turn, also waits for the handoff:
// "/agents resume" would start a run whose output lands in the new session.
func TestHandoffHoldsAgentsCommand(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	m, _ = step(t, m, SendMessageMsg{Content: "/agents resume cp-1"})
	assert.Equal(t, []string{"/agents resume cp-1"}, m.handoffHeld)
	m, cmd := step(t, m, ready)
	assert.Equal(t, []string{"/agents resume cp-1"}, sentMessages(cmd))
	assert.Empty(t, m.handoffHeld)
}

// A quit word typed during a handoff quits, as it does otherwise; it is
// not held and pasted into the notes (review of #352).
func TestHandoffQuitWordQuits(t *testing.T) {
	for _, word := range []string{"exit", "quit", "q", ":q", "EXIT"} {
		m, _ := startTestHandoff(t, nil)
		m, cmd := step(t, m, SendMessageMsg{Content: word})
		assert.Empty(t, m.handoffHeld, "%q was held", word)
		quit := false
		for _, msg := range collectMsgs(cmd) {
			if _, ok := msg.(tea.QuitMsg); ok {
				quit = true
			}
		}
		assert.True(t, quit, "%q during a handoff did not quit", word)
	}
}

// A legacy text command ("help", "clear", ...) typed during a handoff runs
// as a command in the new session; it does not join the notes.
func TestHandoffLegacyCommandRunsInNewSession(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	m, _ = step(t, m, SendMessageMsg{Content: "help"})
	m, cmd := step(t, m, ready)
	assert.Equal(t, "handoff notes for go", m.input.Value())
	assert.Equal(t, []string{"help"}, sentMessages(cmd))
}

// Chat text queued before /handoff follows the notes in the new session's
// input instead of going out ahead of them; a queued command still runs
// there.
func TestHandoffQueuedFollowUpJoinsNotes(t *testing.T) {
	m, c := newCompactTestApp(t)
	_ = c
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m.followUpQueue = []string{"and the parser", "/stats"}
	m.steerQueue = []string{"look at the lexer"}
	m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
	var ready tea.Msg
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(HandoffReadyMsg); ok {
			ready = msg
		}
	}
	require.NotNil(t, ready)
	assert.Empty(t, m.followUpQueue)
	assert.Empty(t, m.steerQueue)

	m, _ = step(t, m, SendMessageMsg{Content: "typed later"})
	m, cmd = step(t, m, ready)
	assert.Equal(t, "handoff notes for go\n\nlook at the lexer\n\nand the parser\n\ntyped later", m.input.Value())
	assert.Equal(t, []string{"/stats"}, sentMessages(cmd))
}

// Esc on an empty input, or Ctrl+C, cancels a running handoff: its context
// is cancelled, the old session stays, and held input is released there.
func TestHandoffCancel(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		m, c := newCompactTestApp(t)
		m = runToolTurn(t, m)
		m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
		c.handoffBlock = true
		before := len(m.chat.GetLLMMessages())
		m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
		require.NotNil(t, cmd)
		assert.Contains(t, strings.ToLower(m.status.text), "esc")

		done := make(chan tea.Msg, 1)
		awaitHandoffReady(cmd, done)

		m, _ = step(t, m, SendMessageMsg{Content: "/stats"})
		m, qcmd := step(t, m, key)
		for _, msg := range collectMsgs(qcmd) {
			_, isQuit := msg.(tea.QuitMsg)
			assert.False(t, isQuit, "%s quit instead of cancelling the handoff", key)
		}
		var ready tea.Msg
		select {
		case ready = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not cancel the handoff's context", key)
		}
		m, cmd = step(t, m, ready)
		assert.False(t, m.handingOff)
		assert.Len(t, m.chat.GetLLMMessages(), before, "the old session was replaced")
		assert.Equal(t, []string{"/stats"}, sentMessages(cmd))
		assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Handoff cancelled"))
	}
}

// awaitHandoffReady runs cmd (and any batch inside it) in the background
// and delivers the HandoffReadyMsg it produces, however long it takes.
func awaitHandoffReady(cmd tea.Cmd, out chan<- tea.Msg) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				awaitHandoffReady(c, out)
			}
			return
		}
		if r, ok := msg.(HandoffReadyMsg); ok {
			out <- r
		}
	}()
}

// Notes that arrive after the user cancelled are not applied.
func TestHandoffCancelledNotesNotApplied(t *testing.T) {
	m, ready := startTestHandoff(t, nil)
	before := len(m.chat.GetLLMMessages())
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = step(t, m, ready)
	assert.False(t, m.handingOff)
	assert.Len(t, m.chat.GetLLMMessages(), before, "a cancelled handoff replaced the session")
	assert.Empty(t, m.input.Value())
}
