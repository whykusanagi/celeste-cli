package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasTick reports whether cmd (batches expanded) delivers a TickMsg.
func hasTick(cmd tea.Cmd) bool {
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(TickMsg); ok {
			return true
		}
	}
	return false
}

// A failed or denied call's card shows "Error: <message>", as before the
// cutover; the model still gets the loop's JSON envelope (the history comes
// from the loop's snapshots, not the card).
func TestFailedToolCardShowsTheErrorMessage(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, ToolStartMsg{ID: "call_a", Name: "tool_a"})
	m, _ = feed(t, m, ToolStartMsg{ID: "call_b", Name: "tool_b"})
	m, _ = feed(t, m, ToolResultMsg{ID: "call_a", Name: "tool_a", IsError: true,
		Content: `{"error":true,"message":"permission denied: write_file","tool":"tool_a"}`})
	m, _ = feed(t, m, ToolResultMsg{ID: "call_b", Name: "tool_b", IsError: true, Content: "not json"})
	calls := m.chat.functionCalls
	require.Len(t, calls, 2)
	assert.Equal(t, "Error: permission denied: write_file", calls[0].Result)
	assert.Equal(t, "Error: not json", calls[1].Result)
}

// Enter starts the spinner at once, so it animates while a
// UserPromptSubmit hook runs, before the loop's first TurnStartMsg.
func TestEnterStartsTheSpinner(t *testing.T) {
	m, _ := newQueueTestApp()
	m, cmd := step(t, m, SendMessageMsg{Content: "go"})
	require.NotNil(t, m.turn)
	assert.True(t, hasTick(cmd), "Enter scheduled no TickMsg: the spinner is frozen until the first request")

	// The first request does not start a second tick chain (the spinner
	// would run at double speed); a later request after tools does start one.
	m, cmd = feed(t, m, TurnStartMsg{Turn: 1})
	assert.False(t, hasTick(cmd), "the first TurnStartMsg started a second tick chain")
	m, _ = feed(t, m, ToolTurnMsg{Text: ""})
	m, cmd = feed(t, m, TurnStartMsg{Turn: 2})
	assert.True(t, hasTick(cmd), "the next request after tools has no spinner")
}

// /plan's "Planning..." stays until the reply starts streaming.
func TestPlanStatusStaysUntilTheReplyStreams(t *testing.T) {
	m, _ := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "/plan build a thing"})
	require.NotNil(t, m.turn)
	assert.Equal(t, "Planning...", m.status.text)
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	assert.Equal(t, "Planning...", m.status.text, "the first request replaced Planning...")
	m, _ = step(t, m, TickMsg{})
	assert.Equal(t, "Planning...", m.status.text, "a tick replaced Planning...")
	m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: "Here", IsFirst: true}})
	assert.NotEqual(t, "Planning...", m.status.text)
	m, _ = step(t, m, TickMsg{})
	assert.NotEqual(t, "Planning...", m.status.text)
}

// While the Stop hook runs (the reply is in, the turn has not ended), the
// status says so instead of "Ready", so input typed then is not a surprise
// when it waits for the turn.
func TestStopHookStatusWhileItRuns(t *testing.T) {
	type order int
	const (
		hookBeforeTypingEnds order = iota
		hookAfterTypingEnds
	)
	for _, o := range []order{hookBeforeTypingEnds, hookAfterTypingEnds} {
		m, _ := newQueueTestApp()
		m = startedTurn(t, m, "go")
		m, _ = feed(t, m, TurnStartMsg{Turn: 1})
		m, _ = feed(t, m, StreamChunkMsg{Chunk: StreamChunk{Content: "hi", IsFirst: true}})
		m, _ = feed(t, m, StreamDoneMsg{FullContent: "hi there", FinishReason: "stop"})
		if o == hookBeforeTypingEnds {
			m, _ = feed(t, m, StopHookStartMsg{})
		}
		for i := 0; i < 100 && m.typingContent != ""; i++ {
			m, _ = step(t, m, TickMsg{})
		}
		require.Empty(t, m.typingContent)
		if o == hookAfterTypingEnds {
			require.True(t, strings.HasPrefix(m.status.text, "Ready"), "status = %q", m.status.text)
			m, _ = feed(t, m, StopHookStartMsg{})
		}
		assert.Equal(t, "Running Stop hook…", m.status.text, "order %d", o)
		assert.True(t, m.turnActive())
		m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
		assert.True(t, strings.HasPrefix(m.status.text, "Ready"), "order %d: status after the turn = %q", o, m.status.text)
	}
}

// A Stop continuation clears the Stop hook status: the next run's spinner
// takes over.
func TestStopContinuationClearsTheStopHookStatus(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, StreamDoneMsg{FullContent: "hi", FinishReason: "stop"})
	for i := 0; i < 100 && m.typingContent != ""; i++ {
		m, _ = step(t, m, TickMsg{})
	}
	m, _ = feed(t, m, StopHookStartMsg{})
	m, _ = feed(t, m, StopContinueMsg{Message: ChatMessage{Role: "user", Content: "more", Metadata: map[string]any{"hidden": true}}, Reason: "more"})
	assert.NotEqual(t, "Running Stop hook…", m.status.text, "the continuation's first events still show the Stop hook")
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	assert.NotEqual(t, "Running Stop hook…", m.status.text)
	m, _ = feed(t, m, StreamDoneMsg{FullContent: "done now", FinishReason: "stop"})
	for i := 0; i < 100 && m.typingContent != ""; i++ {
		m, _ = step(t, m, TickMsg{})
	}
	assert.True(t, strings.HasPrefix(m.status.text, "Ready"), "status = %q", m.status.text)
}

// Esc interrupts a turn while its ask is on the way to the chat: the modal
// opens after the interrupt, and must close when the turn ends, answering
// the ask, instead of staying up and swallowing keys.
func TestStaleModalsCloseWhenTheirTurnEnds(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, m.interrupted)

	ask := make(chan AskResponseMsg, 1)
	m, _ = step(t, m, AskRequestMsg{Question: "which?", Options: []AskOption{{Label: "a"}}, Response: ask})
	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm})
	require.True(t, m.askPrompt.Active())
	require.True(t, m.permissionPrompt.Active())

	m, _ = feed(t, m, TurnDoneMsg{Stop: "interrupted"})
	assert.False(t, m.askPrompt.Active(), "the ask modal outlived its turn")
	assert.False(t, m.permissionPrompt.Active(), "the permission modal outlived its turn")
	select {
	case r := <-ask:
		assert.True(t, r.Cancelled)
	default:
		t.Fatal("the ask was never answered")
	}
	select {
	case r := <-perm:
		assert.Equal(t, "deny", r.Decision)
	default:
		t.Fatal("the permission ask was never answered")
	}

	// Keys reach the chat again.
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Equal(t, "x", m.input.Value())
}

// A modal that belongs to the next turn stays when an older turn's end
// arrives late; a modal opened with no run stays until it is answered.
func TestModalsOfOtherRunsStay(t *testing.T) {
	m, _ := newQueueTestApp()
	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm})
	m = startedTurn(t, m, "go")
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	assert.True(t, m.permissionPrompt.Active(), "a modal with no run was closed by a turn's end")
	assert.Empty(t, perm)
}

// The same for /agent and /orch runs: their end closes their modals.
func TestAgentAndOrchModalsCloseWhenTheRunEnds(t *testing.T) {
	t.Run("agent", func(t *testing.T) {
		m := NewApp(&fakeAgentLLMClient{})
		m, _ = step(t, m, SendMessageMsg{Content: "/agent fix it"})
		perm := make(chan PermissionResponse, 1)
		m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm})
		require.True(t, m.permissionPrompt.Active())
		m, _ = step(t, m, AgentProgressMsg{Kind: AgentProgressError, Text: "context canceled"})
		assert.False(t, m.permissionPrompt.Active(), "the /agent modal outlived its run")
		require.Len(t, perm, 1)
		assert.Equal(t, "deny", (<-perm).Decision)
	})
	t.Run("agent complete", func(t *testing.T) {
		m := NewApp(&fakeAgentLLMClient{})
		m, _ = step(t, m, SendMessageMsg{Content: "/agent fix it"})
		ask := make(chan AskResponseMsg, 1)
		m, _ = step(t, m, AskRequestMsg{Question: "q", Options: []AskOption{{Label: "a"}}, Response: ask})
		m, _ = step(t, m, AgentProgressMsg{Kind: AgentProgressComplete})
		assert.False(t, m.askPrompt.Active(), "the /agent modal outlived its run")
		require.Len(t, ask, 1)
		assert.True(t, (<-ask).Cancelled)
	})
	t.Run("orch", func(t *testing.T) {
		client := &fakeOrchClient{}
		m := NewApp(client)
		m, _ = step(t, m, SendMessageMsg{Content: "/orch say hi"})
		run := client.runs[0]
		perm := make(chan PermissionResponse, 1)
		m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm})
		m, _ = step(t, m, OrchestratorEventMsg{Kind: 7, Run: run})
		assert.False(t, m.permissionPrompt.Active(), "the /orch modal outlived its run")
		require.Len(t, perm, 1)
		assert.Equal(t, "deny", (<-perm).Decision)
	})
	t.Run("orch error", func(t *testing.T) {
		client := &fakeOrchClient{}
		m := NewApp(client)
		m, _ = step(t, m, SendMessageMsg{Content: "/orch say hi"})
		run := client.runs[0]
		perm := make(chan PermissionResponse, 1)
		m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm})
		m, _ = step(t, m, OrchestratorEventMsg{Kind: 8, Text: "boom", Run: run})
		assert.False(t, m.permissionPrompt.Active(), "the /orch modal outlived its run")
		require.Len(t, perm, 1)
	})
}
