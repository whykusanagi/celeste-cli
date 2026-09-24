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

func toolBatch(ids ...string) SkillCallBatchMsg {
	b := SkillCallBatchMsg{AssistantContent: "working"}
	for i, id := range ids {
		name := []string{"tool_a", "tool_b"}[i%2]
		b.Calls = append(b.Calls, SkillCallRequest{
			Call:       FunctionCall{Name: name, Arguments: map[string]any{"i": id}, Status: "executing"},
			ToolCallID: id,
		})
		b.ToolCalls = append(b.ToolCalls, ToolCallInfo{ID: id, Name: name, Arguments: `{"i":"` + id + `"}`})
	}
	return b
}

// Enter during a turn queues a steer instead of firing a concurrent request,
// and doesn't reset the tool-iteration cap (#172).
func TestSendDuringTurnIsQueuedNotSent(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	require.Len(t, client.sendCalls, 1)
	m.clawToolIterations = 3

	m, _ = step(t, m, SendMessageMsg{Content: "also check the tests"})
	assert.Len(t, client.sendCalls, 1, "a second request was sent during the turn")
	assert.Equal(t, []string{"also check the tests"}, m.steerQueue)
	assert.Equal(t, 3, m.clawToolIterations, "queuing reset the tool-iteration cap")
}

// A steer joins the conversation at the next tool boundary: after the tool
// results, before the follow-up request.
func TestSteerInjectedAtToolBoundary(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, toolBatch("call_a"))
	m, _ = step(t, m, SendMessageMsg{Content: "use the v2 API"})
	require.Len(t, client.sendCalls, 1)

	m, _ = step(t, m, SkillResultMsg{Name: "tool_a", Result: `{"ok":true}`, ToolCallID: "call_a"})
	require.Len(t, client.sendCalls, 2, "the follow-up request was not sent")
	assert.Empty(t, m.steerQueue)

	msgs := m.chat.GetLLMMessages()
	require.GreaterOrEqual(t, len(msgs), 2)
	last := msgs[len(msgs)-1]
	assert.Equal(t, "user", last.Role)
	assert.Equal(t, "use the v2 API", last.Content)
	assert.Equal(t, "tool", msgs[len(msgs)-2].Role, "the steer must follow the tool result")
	assert.Equal(t, len(msgs), client.sendCalls[1].messageCount, "the follow-up request must include the steer")
}

// Tab queues a follow-up that is sent once the turn finishes.
func TestFollowUpSentAfterTurnEnds(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, cmd := step(t, m, SendMessageMsg{Content: "then summarise", FollowUp: true})
	assert.Equal(t, []string{"then summarise"}, m.followUpQueue)
	_, sent := queuedSend(cmd)
	assert.False(t, sent, "follow-up sent while the turn was running")

	// Turn ends with an empty reply.
	m, cmd = step(t, m, StreamDoneMsg{})
	next, sent := queuedSend(cmd)
	require.True(t, sent, "follow-up not dispatched when the turn ended")
	assert.Equal(t, "then summarise", next.Content)
	assert.Empty(t, m.followUpQueue)
	assert.True(t, m.dispatchPending)

	m, _ = step(t, m, next)
	assert.False(t, m.dispatchPending)
	assert.Len(t, client.sendCalls, 2)
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

// Esc on an empty input interrupts the stream.
func TestEscInterruptsStream(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	cancelled := false
	m.cancelFunc = func() { cancelled = true }

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, cancelled, "Esc did not cancel the request")
	assert.False(t, m.turnActive())
}

// Esc during serial tools: the running tool finishes, the queued one is
// skipped with a result, and no follow-up request is sent.
func TestEscSkipsQueuedToolsAndFollowUp(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "first"})
	m, _ = step(t, m, toolBatch("call_a", "call_b"))
	require.Len(t, client.executeCalls, 1)

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, m.interrupted)

	m, _ = step(t, m, SkillResultMsg{Name: "tool_a", Result: `{"ok":true}`, ToolCallID: "call_a"})
	assert.Len(t, client.executeCalls, 1, "a queued tool ran after Esc")
	assert.Len(t, client.sendCalls, 1, "a follow-up request was sent after Esc")
	assert.False(t, m.turnActive())

	results := map[string]bool{}
	for _, msg := range m.chat.GetLLMMessages() {
		if msg.Role == "tool" {
			results[msg.ToolCallID] = true
		}
	}
	assert.True(t, results["call_a"] && results["call_b"], "every tool call needs a result, got %v", results)
}
