package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// failingSessions is a disk store whose saves all fail (a full or read-only
// disk).
type failingSessions struct{ *diskSessions }

func (f failingSessions) Save(interface{}) error { return errors.New("disk full") }

// A clear whose save of the current, never-saved session fails must keep that
// session and its chat, and say why: clearing would lose the only copy.
func clearKeepsSessionWhenSaveFails(t *testing.T, input string) {
	t.Helper()
	m, mgr, _ := newSessionTestApp(t)
	m.sessionManager = failingSessions{mgr}
	m.chat = m.chat.AddAssistantMessage("reply from the current session")
	before, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)

	m, _ = step(t, m, SendMessageMsg{Content: input})

	after, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)
	assert.Equal(t, before.ID, after.ID, "%s must keep the current session when its save fails", input)
	text := sessChatText(m)
	assert.Contains(t, text, "message from the current session", "%s must keep the chat", input)
	assert.Contains(t, text, "reply from the current session", "%s must keep the chat", input)
	assert.Contains(t, text, "disk full", "%s must show the save error", input)
	assert.NotContains(t, text, "Session cleared", "%s must not report a clear", input)
}

func TestSessionClearKeepsSessionWhenSaveFails(t *testing.T) {
	clearKeepsSessionWhenSaveFails(t, "/session clear")
}

func TestClearKeepsSessionWhenSaveFails(t *testing.T) {
	clearKeepsSessionWhenSaveFails(t, "/clear")
}

func TestSessionNewKeepsSessionWhenSaveFails(t *testing.T) {
	clearKeepsSessionWhenSaveFails(t, "/session new")
}

// Resuming another session replaces this one the same way.
func TestSessionResumeKeepsSessionWhenSaveFails(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	m.sessionManager = failingSessions{mgr}
	before, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)

	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})

	after, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)
	assert.Equal(t, before.ID, after.ID)
	text := sessChatText(m)
	assert.Contains(t, text, "message from the current session")
	assert.NotContains(t, text, "message from the other session")
	assert.Contains(t, text, "disk full")
}
