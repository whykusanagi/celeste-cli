package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// diskSessions adapts config.SessionManager to SessionManager, as the chat's
// adapter does, and counts saves.
type diskSessions struct {
	mgr   *config.SessionManager
	saves int
}

func (d *diskSessions) NewSession() interface{} { return d.mgr.NewSession() }
func (d *diskSessions) Save(s interface{}) error {
	d.saves++
	cs, ok := s.(*config.Session)
	if !ok {
		return fmt.Errorf("invalid session type")
	}
	return d.mgr.Save(cs)
}
func (d *diskSessions) Load(id string) (interface{}, error) { return d.mgr.Load(id) }
func (d *diskSessions) List() ([]interface{}, error) {
	ss, err := d.mgr.List()
	if err != nil {
		return nil, err
	}
	out := make([]interface{}, len(ss))
	for i := range ss {
		out[i] = &ss[i]
	}
	return out, nil
}
func (d *diskSessions) Delete(id string) error { return d.mgr.Delete(id) }
func (d *diskSessions) MergeSessions(a, b interface{}) interface{} {
	sa, ok1 := a.(*config.Session)
	sb, ok2 := b.(*config.Session)
	if !ok1 || !ok2 {
		return nil
	}
	return d.mgr.MergeSessions(sa, sb)
}

func newSessionTestApp(t *testing.T) (AppModel, *diskSessions, *config.Session) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mgr := &diskSessions{mgr: config.NewSessionManager()}

	other := mgr.mgr.NewSession()
	other.Name = "Other notes"
	other.Messages = []config.SessionMessage{{Role: "user", Content: "message from the other session"}}
	require.NoError(t, mgr.mgr.Save(other))
	mgr.saves = 0

	current := mgr.mgr.NewSession()
	m := NewApp(&fakeCompactClient{}).SetSessionManager(mgr, current)
	m.chat = m.chat.AddUserMessage("message from the current session")
	return m, mgr, other
}

func sessChatText(m AppModel) string {
	var sb strings.Builder
	for _, msg := range m.chat.GetMessages() {
		sb.WriteString(msg.Content + "\n")
	}
	return sb.String()
}

func TestSessionAloneOpensThePicker(t *testing.T) {
	m, _, _ := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "/session"})
	assert.Equal(t, "sessions", m.viewMode)
	assert.NotNil(t, m.sessionPanel)
}

func TestSessionMergeRunsInsteadOfOpeningThePicker(t *testing.T) {
	m, _, other := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "/session merge " + other.ID})
	assert.NotEqual(t, "sessions", m.viewMode, "a subcommand must not open the picker")
	text := sessChatText(m)
	assert.Contains(t, text, "message from the other session")
	assert.Contains(t, text, "message from the current session")
}

func TestSessionResumeRunsInsteadOfOpeningThePicker(t *testing.T) {
	m, _, other := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	assert.NotEqual(t, "sessions", m.viewMode)
	assert.Contains(t, sessChatText(m), "message from the other session")
	assert.NotContains(t, sessChatText(m), "message from the current session")
}

func TestSessionResumeByName(t *testing.T) {
	m, _, _ := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: `/session resume Other notes`})
	assert.Contains(t, sessChatText(m), "message from the other session")
}

func TestSessionNewKeepsTheName(t *testing.T) {
	m, _, _ := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "/session new Planning notes"})
	s, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)
	assert.Equal(t, "Planning notes", s.Name)
}

func TestContextCompactSavesTheSession(t *testing.T) {
	m, client := newCompactTestApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mgr := &diskSessions{mgr: config.NewSessionManager()}
	m = m.SetSessionManager(mgr, mgr.mgr.NewSession())
	m = runToolTurn(t, m)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	mgr.saves = 0

	m, _ = step(t, m, SendMessageMsg{Content: "/context compact"})
	require.NotEmpty(t, client.calls)
	require.Equal(t, "[pruned call_a]", toolContent(m, "call_a"))
	assert.Positive(t, mgr.saves, "/context compact must save the compacted session")
}

// 2.0 W4 rulings 1-2: /session list shows this project's sessions first,
// marked; a new session records the chat's workspace.
func TestSessionListShowsThisProjectFirst(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	ws := t.TempDir()
	m = m.SetWorkDir(ws)
	here := mgr.mgr.NewSession()
	here.Name = "Here notes"
	here.Workspace = ws
	here.Messages = []config.SessionMessage{{Role: "user", Content: "hi"}}
	require.NoError(t, mgr.mgr.Save(here))
	// The other session is newer, but in no project.
	other.Name = "Other notes"
	require.NoError(t, mgr.mgr.Save(other))

	m, _ = step(t, m, SendMessageMsg{Content: "/session list"})
	text := sessChatText(m)
	iHere, iOther := strings.Index(text, "Here notes"), strings.Index(text, "Other notes")
	require.True(t, iHere >= 0 && iOther >= 0, text)
	assert.Less(t, iHere, iOther, "this project's session must come first")
	assert.Contains(t, text, "Here notes ("+here.ID+") (this project)")
	assert.NotContains(t, text, "Other notes ("+other.ID+") (this project)")

	m, _ = step(t, m, SendMessageMsg{Content: "/session new Fresh"})
	assert.Equal(t, ws, m.currentSession.GetWorkspace())
}
