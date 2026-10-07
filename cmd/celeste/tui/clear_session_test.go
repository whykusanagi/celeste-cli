package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// clearKeepsTranscript runs a clear through send and checks the cleared
// session's file still holds the whole chat, so it can be resumed (#398 T1).
func clearKeepsTranscript(t *testing.T, send func(AppModel) AppModel) {
	t.Helper()
	m, mgr, _ := newSessionTestApp(t)
	m.chat = m.chat.AddAssistantMessage("reply from the current session")
	old, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)

	m = send(m)

	now, ok := m.currentSession.SummarizeRaw().(config.SessionSummary)
	require.True(t, ok)
	require.NotEqual(t, old.ID, now.ID, "clear must start a new session")
	assert.NotContains(t, sessChatText(m), "message from the current session", "clear must empty the chat")

	saved, err := mgr.mgr.Load(old.ID)
	require.NoError(t, err)
	var contents []string
	for _, msg := range saved.Messages {
		contents = append(contents, msg.Content)
	}
	assert.Equal(t, []string{"message from the current session", "reply from the current session"}, contents,
		"the cleared session must keep its transcript")
}

func TestClearSavesTheOldSessionTranscript(t *testing.T) {
	clearKeepsTranscript(t, func(m AppModel) AppModel {
		m, _ = step(t, m, SendMessageMsg{Content: "/clear"})
		return m
	})
}

func TestMenuClearSavesTheOldSessionTranscript(t *testing.T) {
	clearKeepsTranscript(t, func(m AppModel) AppModel {
		m.viewMode = "menu"
		m, cmd := step(t, m, menuItemSelectedMsg{command: "clear"})
		require.NotNil(t, cmd)
		m, _ = step(t, m, cmd())
		return m
	})
}

func TestClearWithoutSessionManagerStillClearsTheChat(t *testing.T) {
	m := NewApp(&fakeCompactClient{})
	m.chat = m.chat.AddUserMessage("message before clear")
	m, _ = step(t, m, SendMessageMsg{Content: "/clear"})
	assert.NotContains(t, sessChatText(m), "message before clear")
}

// Typing bare "clear" (terminal habit) must not wipe the session either.
func TestBareClearSavesTheOldSessionTranscript(t *testing.T) {
	clearKeepsTranscript(t, func(m AppModel) AppModel {
		m, _ = step(t, m, SendMessageMsg{Content: "clear"})
		return m
	})
}

func TestBareClearWithoutSessionManagerStillClearsTheChat(t *testing.T) {
	m := NewApp(&fakeCompactClient{})
	m.chat = m.chat.AddUserMessage("message before clear")
	m, _ = step(t, m, SendMessageMsg{Content: "clear"})
	assert.NotContains(t, sessChatText(m), "message before clear")
}

// /session clear saves the old session before it switches, even one never
// saved before.
func TestSessionClearSavesTheOldSessionTranscript(t *testing.T) {
	clearKeepsTranscript(t, func(m AppModel) AppModel {
		m, _ = step(t, m, SendMessageMsg{Content: "/session clear"})
		return m
	})
}
