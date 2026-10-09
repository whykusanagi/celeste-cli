package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
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

// 2.0 W4 ruling 3: /fork saves the session, continues in a copy (messages,
// tool calls, provider blocks) and leaves the original as it was.
func TestForkCopiesTheSessionAndSwitches(t *testing.T) {
	m, mgr, _ := newSessionTestApp(t)
	ws := t.TempDir()
	m = m.SetWorkDir(ws)
	m.currentSession.SetName("Design talk")
	m.model = "model-x"
	pb := mustBlocks(t, keyA, `{"type":"text","text":"done"}`)
	m.chat = m.chat.Clear().RestoreMessages([]ChatMessage{
		prompt("read a"),
		{Role: "assistant", ToolCalls: []ToolCallInfo{{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "alpha"},
		AttachProviderBlocks(ChatMessage{Role: "assistant", Content: "done"}, pb),
	})
	orig := m.currentSession.(*config.Session)
	origID := orig.ID

	m, _ = step(t, m, SendMessageMsg{Content: "/fork"})
	fork, ok := m.currentSession.(*config.Session)
	require.True(t, ok)
	require.NotEqual(t, origID, fork.ID)
	assert.Equal(t, "fork of Design talk", fork.Name)
	assert.Equal(t, ws, fork.Workspace)
	assert.Equal(t, "model-x", fork.GetModel())
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(),
		"Forked into "+fork.ID+"; the original is unchanged (/session resume "+origID+" to go back)."))

	saved, err := mgr.mgr.Load(origID)
	require.NoError(t, err)
	require.Len(t, saved.Messages, 4)
	forked, err := mgr.mgr.Load(fork.ID)
	require.NoError(t, err)
	require.Equal(t, len(saved.Messages), len(forked.Messages))
	for i := range saved.Messages {
		a, b := saved.Messages[i], forked.Messages[i]
		assert.Equal(t, a.Role, b.Role)
		assert.Equal(t, a.Content, b.Content)
		assert.Equal(t, a.ToolCalls, b.ToolCalls)
		assert.Equal(t, a.ToolCallID, b.ToolCallID)
		assert.Equal(t, a.ProviderBlocks, b.ProviderBlocks)
	}
	require.NotNil(t, forked.Messages[3].ProviderBlocks, "provider blocks are copied")

	// Later messages go to the fork only.
	m.chat = m.chat.AddUserMessage("only in the fork")
	m.persistSession()
	saved, _ = mgr.mgr.Load(origID)
	assert.Len(t, saved.Messages, 4)
	forked, _ = mgr.mgr.Load(fork.ID)
	assert.Len(t, forked.Messages, 5)
}

// #315: the /session list footer keeps its <id> and <name> placeholders
// when rendered (the markdown renderer took them for HTML tags).
func TestSessionListFooterRendersPlaceholders(t *testing.T) {
	m, _, _ := newSessionTestApp(t)
	m, _ = step(t, m, SendMessageMsg{Content: "/session list"})
	msgs := m.chat.GetMessages()
	require.NotEmpty(t, msgs)
	last := msgs[len(msgs)-1]
	require.Contains(t, last.Content, "Commands:")

	out := stripANSI(m.chat.renderMessageOpt(last, 100, false))
	for _, want := range []string{
		"/session resume <id>",
		`/session resume "<name>"`,
		"/session rename <id> <name>",
		"/session delete <id>",
	} {
		assert.Contains(t, out, want)
	}
	// One command per line, indented as written.
	assert.Contains(t, out, "\n  /session delete <id>")
}

// Aikido 806869764 (review): /session resume does not switch the client, so
// the header keeps the endpoint in use and the chat says the session's
// endpoint was not taken.
func TestSessionResumeKeepsTheEndpointInUse(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	require.NoError(t, mgr.mgr.Save(other))
	m = m.WithEndpoint("openai")
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	assert.Equal(t, "openai", m.endpoint)
	assert.Equal(t, "openai", m.header.endpoint)
	assert.Contains(t, sessChatText(m), "venice")
	assert.Contains(t, sessChatText(m), "/endpoint venice")
}

// A session resumed on another endpoint than its own keeps its endpoint
// and model through passive saves (CodeRabbit, #427): saving it before a
// switch or on quit does not rewrite it to the endpoint in use, so a
// later startup resume still goes to the session's profile. An explicit
// endpoint change is recorded.
func TestSessionResumeKeepsTheSessionsEndpointOnPassiveSaves(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	other.SetModel("venice-model")
	require.NoError(t, mgr.mgr.Save(other))
	m = m.WithEndpoint("openai")
	m.model = "gpt-model"
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	require.Equal(t, "openai", m.endpoint)

	m.persistSession()
	saved, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "venice", saved.GetEndpoint(), "a passive save rewrote the session's endpoint")
	assert.Equal(t, "venice-model", saved.GetModel(), "a passive save rewrote the session's model")

	// Switching endpoints is recorded.
	m.endpoint = "grok"
	m.persistSession()
	saved, err = mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "grok", saved.GetEndpoint())
	assert.Equal(t, "gpt-model", saved.GetModel())
}

// A turn sent on the endpoint in use makes it the session's: the resumed
// conversation continues there, so its saves record that endpoint and model
// (review follow-up).
func TestSessionResumeContinuedOnTheEndpointInUseRecordsIt(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	other.SetModel("venice-model")
	require.NoError(t, mgr.mgr.Save(other))
	m = m.WithEndpoint("openai")
	m.model = "gpt-model"
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	m, _ = step(t, m, SendMessageMsg{Content: "carry on"})
	saved, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "openai", saved.GetEndpoint())
	assert.Equal(t, "gpt-model", saved.GetModel())
}

// The same at startup: a session whose profile could not be loaded stays
// on the startup endpoint without losing its own.
func TestStartupResumeFallbackKeepsTheSessionsEndpoint(t *testing.T) {
	_, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	require.NoError(t, mgr.mgr.Save(other))
	m := NewApp(&fakeCompactClient{}).WithEndpoint("openai").SetSessionManager(mgr, other)
	m.persistSession()
	saved, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "venice", saved.GetEndpoint())
	assert.Equal(t, "openai", m.endpoint)
}

// An explicit /set-model on a session resumed from another endpoint is a
// choice made on the endpoint in use, and is recorded: a save after it
// keeps that model (with the endpoint it was chosen on), so a later resume
// does not bring the old one back (CodeRabbit review of #428).
func TestSessionResumeExplicitModelChoiceIsRecorded(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	other.SetModel("venice-model")
	require.NoError(t, mgr.mgr.Save(other))
	m = m.WithEndpoint("openai")
	m.model = "gpt-model"
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	require.Equal(t, "openai", m.endpoint)

	m, _ = step(t, m, SendMessageMsg{Content: "/set-model chosen-model --force"})
	require.Equal(t, "chosen-model", m.model)
	m.persistSession()
	saved, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "chosen-model", saved.GetModel(), "an explicit model choice was not recorded")
	assert.Equal(t, "openai", saved.GetEndpoint())
	assert.True(t, saved.GetModelPinned(), "the --force pin was not recorded")
}

// A model picked from the model picker on a session resumed from another
// endpoint is an explicit choice too, and is recorded like /set-model: the
// save keeps the picked model, the endpoint it was picked on and no pin.
func TestSessionResumePickerChoiceIsRecorded(t *testing.T) {
	m, mgr, other := newSessionTestApp(t)
	other.SetEndpoint("venice")
	other.SetModel("venice-model")
	require.NoError(t, mgr.mgr.Save(other))
	m = m.WithEndpoint("openai")
	m, _ = step(t, m, SendMessageMsg{Content: "/session resume " + other.ID})
	require.Equal(t, "openai", m.endpoint)

	m.llmClient = &endpointClient{ep: ActiveEndpoint{Provider: "openai"}}
	m.selectorActive = true
	m, _ = step(t, m, SelectorResultMsg{Selected: &SelectorItem{ID: "picked-model"}})
	require.Equal(t, "picked-model", m.model)
	m.persistSession()
	saved, err := mgr.mgr.Load(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "picked-model", saved.GetModel(), "a picked model was not recorded")
	assert.Equal(t, "openai", saved.GetEndpoint())
	assert.False(t, saved.GetModelPinned())
}
