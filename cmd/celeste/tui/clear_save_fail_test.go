package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

// failSecondSave saves once, then fails: the old session saves before a
// merge, the merged one does not.
type failSecondSave struct {
	*diskSessions
	n int
}

func (f *failSecondSave) Save(s interface{}) error {
	f.n++
	if f.n > 1 {
		return errors.New("disk full")
	}
	return f.diskSessions.Save(s)
}

// A merge whose merged session fails to save says so instead of reporting
// only success.
func TestSessionMergeReportsAFailedSave(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	m.sessionManager = &failSecondSave{diskSessions: mgr}
	m, _ = step(t, m, SendMessageMsg{Content: "/session merge " + other.ID})
	text := sessChatText(m)
	assert.Contains(t, text, "Merged sessions")
	assert.Contains(t, text, "disk full", "the failed save of the merged session must show")
}

// A handoff whose save of the current session fails must keep that session
// and its chat, show the error, and leave the notes in the input so they are
// not lost either.
func TestHandoffKeepsSessionWhenSaveFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m, _ := newCompactTestApp(t)
	mgr := &diskSessions{mgr: config.NewSessionManager()}
	old := mgr.mgr.NewSession()
	m = m.SetSessionManager(failingSessions{mgr}, old)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = sized.(AppModel)

	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	beforeMsgs := len(m.chat.GetLLMMessages())
	require.Positive(t, beforeMsgs)
	beforeText := sessChatText(m)

	m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
	m = runCmd(t, m, cmd)

	cur, ok := m.currentSession.(*config.Session)
	require.True(t, ok)
	assert.Equal(t, old.ID, cur.ID, "/handoff must keep the current session when its save fails")
	assert.Len(t, m.chat.GetLLMMessages(), beforeMsgs, "/handoff must keep the chat")
	text := sessChatText(m)
	assert.Contains(t, text, beforeText)
	assert.Contains(t, text, "disk full", "the save error must show")
	assert.NotContains(t, text, "New session started")
	assert.Equal(t, "handoff notes for go", m.input.Value(), "the notes stay in the input")
}
