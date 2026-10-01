package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A request tagged with a turn that has already ended (its TurnDoneMsg was
// handled first) is answered and never shown (2.0 F2e). Before, the modal
// opened with no owner and stayed until the user answered it.
func TestRequestOfAnEndedTurnIsAnsweredNotShown(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	ended := RunOwner{Kind: OwnerTurn, Run: m.turnRun}
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})

	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm, Owner: ended})
	ask := make(chan AskResponseMsg, 1)
	m, _ = step(t, m, AskRequestMsg{Question: "q", Options: []AskOption{{Label: "a"}}, Response: ask, Owner: ended})
	assert.False(t, m.permissionPrompt.Active(), "a request of an ended turn was shown")
	assert.False(t, m.askPrompt.Active(), "an ask of an ended turn was shown")
	require.Len(t, perm, 1)
	assert.Equal(t, "deny", (<-perm).Decision)
	require.Len(t, ask, 1)
	assert.True(t, (<-ask).Cancelled)
}

// A request from an ended turn that arrives during the next one is answered,
// not shown as the new turn's; the new turn's own request is shown and
// closes with it.
func TestRequestArrivingDuringTheNextTurnKeepsItsOwner(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "one")
	first := RunOwner{Kind: OwnerTurn, Run: m.turnRun}
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	m = startedTurn(t, m, "two")
	second := RunOwner{Kind: OwnerTurn, Run: m.turnRun}

	stale := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: stale, Owner: first})
	assert.False(t, m.permissionPrompt.Active(), "the first turn's request was shown during the second")
	require.Len(t, stale, 1)

	own := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: own, Owner: second})
	require.True(t, m.permissionPrompt.Active())
	assert.Equal(t, second, m.permissionOwner)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
	assert.False(t, m.permissionPrompt.Active())
	require.Len(t, own, 1)
}

// A request whose run was cancelled (Esc) before it arrived is answered at
// once, while its turn is still winding down.
func TestRequestOfACancelledRunIsAnswered(t *testing.T) {
	m, _ := newQueueTestApp()
	m = startedTurn(t, m, "go")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm,
		Owner: RunOwner{Kind: OwnerTurn, Run: m.turnRun}, Done: ctx.Done()})
	assert.False(t, m.permissionPrompt.Active())
	require.Len(t, perm, 1)
	assert.Equal(t, "deny", (<-perm).Decision)
}

// /agent runs are numbered: a request of an earlier /agent run is not
// shown during a later one.
func TestAgentRunsAreTagged(t *testing.T) {
	m := NewApp(&fakeAgentLLMClient{})
	m, _ = step(t, m, SendMessageMsg{Content: "/agent one"})
	first := m.currentRun()
	require.Equal(t, OwnerAgent, first.Kind)
	m, _ = step(t, m, AgentProgressMsg{Kind: AgentProgressComplete})
	m, _ = step(t, m, SendMessageMsg{Content: "/agent two"})
	require.NotEqual(t, first, m.currentRun())
	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm, Owner: first})
	assert.False(t, m.permissionPrompt.Active())
	require.Len(t, perm, 1)
}

func TestRunOwnerRoundTripsThroughContext(t *testing.T) {
	o := RunOwner{Kind: OwnerOrch, Run: 3}
	assert.Equal(t, o, RunOwnerFrom(WithRunOwner(context.Background(), o)))
	assert.Equal(t, RunOwner{}, RunOwnerFrom(context.Background()))
	assert.Equal(t, RunOwner{}, RunOwnerFrom(nil)) //nolint:staticcheck // nil is part of the contract
}

// An /agent run cancelled with Ctrl+C keeps sending until its goroutine
// notices; a second /agent may start first. The first run's late messages
// must not end or steer the second (2.0 F2e review): they carry their run.
func TestStaleAgentRunMessagesAreDropped(t *testing.T) {
	m := NewApp(&fakeAgentLLMClient{})
	m, _ = step(t, m, SendMessageMsg{Content: "/agent one"})
	first := m.agentRun
	_, cancelFirst := context.WithCancel(context.Background())
	m, _ = step(t, m, StreamStartMsg{Cancel: cancelFirst, AgentRun: first})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.False(t, m.turnActive(), "Ctrl+C left the /agent run blocking input")

	m, _ = step(t, m, SendMessageMsg{Content: "/agent two"})
	second := m.agentRun
	require.NotEqual(t, first, second)

	staleCancelled := false
	m, _ = step(t, m, StreamStartMsg{Cancel: func() { staleCancelled = true }, AgentRun: first})
	assert.True(t, staleCancelled, "a stale /agent run's cancel was not called")
	assert.Nil(t, m.cancelFunc, "a stale /agent run's cancel replaced the current one")

	m, _ = step(t, m, AgentProgressMsg{Kind: AgentProgressComplete, AgentRun: first})
	assert.True(t, m.agentActive, "the first run's completion ended the second")
	assert.Equal(t, RunOwner{Kind: OwnerAgent, Run: second}, m.currentRun())
	assert.True(t, m.streaming, "the first run's completion stopped the second's spinner")

	m, _ = step(t, m, AgentProgressMsg{Kind: AgentProgressComplete, AgentRun: second})
	assert.False(t, m.agentActive)
}

// allRunsClient runs chat turns (fakeToolLLMClient), /agent and /orch.
type allRunsClient struct{ fakeToolLLMClient }

func (*allRunsClient) RunAgentCommand([]string, uint64) tea.Cmd      { return nil }
func (*allRunsClient) RunOrchestratorCommand(string, uint64) tea.Cmd { return nil }

// An /agent run interrupted with Esc or Ctrl+C has ended for the chat: its
// late terminal message (error, completion, or an /agent resume result)
// must not end or cancel the /orch run or chat turn started after it
// (2.0 F2e review I1). Before, agentActive stayed set until the next
// /agent, so the late message cleared the new run's cancel and spinner and
// posted a bogus agent error.
func TestLateAgentMessagesLeaveTheNextRunAlone(t *testing.T) {
	late := map[string]func(run uint64) tea.Msg{
		"error": func(r uint64) tea.Msg {
			return AgentProgressMsg{Kind: AgentProgressError, Text: "context canceled", AgentRun: r}
		},
		"complete": func(r uint64) tea.Msg {
			return AgentProgressMsg{Kind: AgentProgressComplete, Text: "late", AgentRun: r}
		},
		"result": func(r uint64) tea.Msg {
			return AgentCommandResultMsg{Output: "late result", Err: errors.New("context canceled"), AgentRun: r}
		},
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		for _, next := range []string{"orch", "turn"} {
			for name, msgOf := range late {
				t.Run(key.String()+"/"+next+"/"+name, func(t *testing.T) {
					m := NewApp(&allRunsClient{})
					m.skillsEnabled = true
					m, _ = step(t, m, SendMessageMsg{Content: "/agent one"})
					agentRun := m.agentRun
					m, _ = step(t, m, StreamStartMsg{Cancel: func() {}, AgentRun: agentRun})
					m, _ = step(t, m, key)
					assert.False(t, m.agentActive, "%s left the /agent run active", key)

					nextCancelled := false
					if next == "orch" {
						m, _ = step(t, m, SendMessageMsg{Content: "/orch build"})
						require.NotZero(t, m.orchRun)
						m, _ = step(t, m, StreamStartMsg{Cancel: func() { nextCancelled = true }, Run: m.orchRun})
						require.NotNil(t, m.cancelFunc)
					} else {
						m = startedTurn(t, m, "go")
					}
					before := len(m.chat.GetMessages())

					m, _ = step(t, m, msgOf(agentRun))
					assert.True(t, m.streaming, "a late /agent message stopped the %s's spinner", next)
					assert.Len(t, m.chat.GetMessages(), before, "a late /agent message posted to the chat")
					if next == "orch" {
						require.NotNil(t, m.cancelFunc, "a late /agent message dropped the /orch run's cancel")
						m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
						assert.True(t, nextCancelled, "Ctrl+C no longer cancels the /orch run")
					} else {
						assert.NotNil(t, m.turn, "a late /agent message ended the chat turn")
					}
				})
			}
		}
	}
}

// /agent resume's result belongs to its run: a late one does not end a
// newer /agent run or deny its open modal (2.0 F2e review I1).
func TestLateAgentResultDoesNotEndANewerAgentRun(t *testing.T) {
	m := NewApp(&allRunsClient{})
	m, _ = step(t, m, SendMessageMsg{Content: "/agent resume abc"})
	first := m.agentRun
	m, _ = step(t, m, StreamStartMsg{Cancel: func() {}, AgentRun: first})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	m, _ = step(t, m, SendMessageMsg{Content: "/agent two"})
	second := m.agentRun
	perm := make(chan PermissionResponse, 1)
	m, _ = step(t, m, PermissionRequestMsg{ToolName: "write_file", Response: perm, Owner: RunOwner{Kind: OwnerAgent, Run: second}})
	require.True(t, m.permissionPrompt.Active())

	m, _ = step(t, m, AgentCommandResultMsg{Output: "resumed", AgentRun: first})
	assert.True(t, m.agentActive, "a late resume result ended the newer /agent run")
	assert.True(t, m.permissionPrompt.Active(), "a late resume result denied the newer run's modal")
	assert.Empty(t, perm)

	m, _ = step(t, m, AgentCommandResultMsg{Output: "done", AgentRun: second})
	assert.False(t, m.agentActive)
}
