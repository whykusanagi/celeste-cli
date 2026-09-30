package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectMsgs runs a command tree (expanding batches) and returns the messages
// it produces, skipping commands that don't finish promptly.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(300 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func queuedSend(cmd tea.Cmd) (SendMessageMsg, bool) {
	for _, msg := range collectMsgs(cmd) {
		if s, ok := msg.(SendMessageMsg); ok {
			return s, true
		}
	}
	return SendMessageMsg{}, false
}

func newQueueTestApp() (AppModel, *fakeToolLLMClient) {
	client := &fakeToolLLMClient{skills: []SkillDefinition{
		{Name: "tool_a", Description: "A"},
		{Name: "tool_b", Description: "B"},
	}}
	m := NewApp(client)
	m.skillsEnabled = true
	return m, client
}

func step(t *testing.T, m AppModel, msg tea.Msg) (AppModel, tea.Cmd) {
	t.Helper()
	model, cmd := m.Update(msg)
	return model.(AppModel), cmd
}

// Enter during a turn steers the running loop instead of starting a second
// request (#172).
func TestSendDuringTurnSteersTheRunningTurn(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, SendMessageMsg{Content: "also check the tests"})
	require.Len(t, client.turns, 1, "a second request was started during the turn")
	assert.Equal(t, []string{"also check the tests"}, client.turns[0].steers)
	assert.Equal(t, 1, m.DebugQueued())
}

// The steer shows in the chat when the loop joins it.
func TestSteerJoinsWhenTheLoopJoinsIt(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m = toolTurn(t, m, "call_a")
	m, _ = step(t, m, SendMessageMsg{Content: "use the v2 API"})
	m, _ = feed(t, m, SteeredMsg{Message: ChatMessage{Role: "user", Content: "use the v2 API", Metadata: map[string]any{MetaPromptHookDone: true}}})
	msgs := m.chat.GetLLMMessages()
	assert.Equal(t, "use the v2 API", msgs[len(msgs)-1].Content)
	assert.Equal(t, "tool", msgs[len(msgs)-2].Role, "the steer follows the tool result")
	assert.Equal(t, 0, m.DebugQueued())
}

// Tab queues a follow-up that is sent once the turn finishes.
func TestFollowUpSentAfterTurnEnds(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, cmd := step(t, m, SendMessageMsg{Content: "then summarise", FollowUp: true})
	assert.Equal(t, []string{"then summarise"}, m.followUpQueue)
	_, sent := queuedSend(cmd)
	assert.False(t, sent, "follow-up sent while the turn was running")

	m, cmd = feed(t, m, TurnDoneMsg{Stop: "done"})
	next, sent := queuedSend(cmd)
	require.True(t, sent, "follow-up not dispatched when the turn ended")
	assert.Equal(t, "then summarise", next.Content)
	m, _ = step(t, m, next)
	assert.Len(t, client.turns, 2)
}

// Commands typed during a turn wait for it, except /agents.
func TestCommandsDuringTurn(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, SendMessageMsg{Content: "/clear"})
	assert.Equal(t, []string{"/clear"}, m.followUpQueue, "a command must wait as a follow-up, not steer")
	assert.True(t, runsDuringTurn("/agents"))
	assert.True(t, runsDuringTurn("/agents kill mizu"))
	assert.False(t, runsDuringTurn("/agent do something"))
}

// Esc cancels the running turn; it stays active until the loop reports the
// end, so no second turn starts over it.
func TestEscCancelsTheTurn(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, client.turns[0].cancelled, "Esc did not cancel the turn")
	assert.True(t, m.interrupted)
	assert.True(t, m.turnActive(), "the turn ends when the loop says so")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	assert.False(t, m.turnActive())
}

// Review Focus 2: a steer the loop never joined comes back and is sent next,
// once.
func TestEscReturnsLeftoverSteersAndSendsThemNext(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, SendMessageMsg{Content: "steer text"})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, cmd := feed(t, m, TurnDoneMsg{Stop: "interrupted", Leftover: []string{"steer text"}})
	next, sent := queuedSend(cmd)
	require.True(t, sent)
	assert.Equal(t, "steer text", next.Content)
	m, _ = step(t, m, next)
	require.Len(t, client.turns, 2)
	assert.Equal(t, 0, m.DebugQueued(), "the steer must not be queued twice")
}

// Review Focus 6: Esc during the first request, after the loop checked the
// prompt: the next turn sends it marked checked, so UserPromptSubmit does
// not run on it again (loop TestLoopCheckPromptSkipsCheckedAndHidden).
func TestEscDuringTheFirstRequestKeepsThePromptChecked(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	checked := append([]ChatMessage(nil), m.chat.GetLLMMessages()...)
	checked[0].Metadata = map[string]any{MetaPromptHookDone: true}
	m, _ = feed(t, m, HistoryMsg{History: checked}) // EventPromptsChecked
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	m, _ = step(t, m, SendMessageMsg{Content: "again"})
	require.Len(t, client.turns, 2)
	first := client.turns[1].req.History[0]
	assert.Equal(t, "first", first.Content)
	assert.Equal(t, true, first.Metadata[MetaPromptHookDone], "the hook would run on the first prompt again")
}
