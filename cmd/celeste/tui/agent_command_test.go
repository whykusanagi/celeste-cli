package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentLLMClient struct {
	agentArgs [][]string
	waitCalls int
}

func (f *fakeAgentLLMClient) SendMessage(messages []ChatMessage, tools []SkillDefinition) tea.Cmd {
	return nil
}

func (f *fakeAgentLLMClient) GetSkills() []SkillDefinition {
	return nil
}

func (f *fakeAgentLLMClient) ExecuteSkill(name string, args map[string]any, toolCallID string) tea.Cmd {
	return nil
}

func (f *fakeAgentLLMClient) RunAgentCommand(args []string) tea.Cmd {
	copied := append([]string{}, args...)
	f.agentArgs = append(f.agentArgs, copied)
	return nil
}

func (f *fakeAgentLLMClient) WaitAgentEvent() tea.Cmd {
	f.waitCalls++
	return nil
}

func TestAgentCommandDispatchesToRunnerAndRendersResult(t *testing.T) {
	client := &fakeAgentLLMClient{}
	m := NewApp(client)

	model, _ := m.Update(SendMessageMsg{Content: "/agent fix flaky tests"})
	m = model.(AppModel)

	require.Len(t, client.agentArgs, 1)
	assert.Equal(t, []string{"fix", "flaky", "tests"}, client.agentArgs[0])
	assert.True(t, m.streaming)

	model, _ = m.Update(AgentCommandResultMsg{
		Output: "Run ID: 20260303-123456\nStatus: completed",
	})
	m = model.(AppModel)

	assert.False(t, m.streaming)
	assert.Equal(t, "Agent run complete", m.status.text)
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Run ID: 20260303-123456"))
}

func TestAgentCommandShowsUsageWhenNoArgs(t *testing.T) {
	client := &fakeAgentLLMClient{}
	m := NewApp(client)

	model, _ := m.Update(SendMessageMsg{Content: "/agent"})
	m = model.(AppModel)

	assert.Len(t, client.agentArgs, 0)
	assert.False(t, m.streaming)
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Usage: /agent <goal>"))
}

func TestAgentCommandRequiresRunnerSupport(t *testing.T) {
	client := &fakeToolLLMClient{}
	m := NewApp(client)

	model, _ := m.Update(SendMessageMsg{Content: "/agent create tests"})
	m = model.(AppModel)

	assert.False(t, m.streaming)
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "/agent is unavailable"))
}

func TestAgentCommandResultErrorSetsStatus(t *testing.T) {
	client := &fakeAgentLLMClient{}
	m := NewApp(client)

	model, _ := m.Update(SendMessageMsg{Content: "/agent create benchmark suite"})
	m = model.(AppModel)
	assert.True(t, m.streaming)

	model, _ = m.Update(AgentCommandResultMsg{
		Output: "Run ID: run-1\nStatus: failed",
		Err:    errors.New("agent finished with status failed"),
	})
	m = model.(AppModel)

	assert.False(t, m.streaming)
	assert.True(t, strings.Contains(m.status.text, "Agent error:"))
	assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Status: failed"))
}

func TestAgentEventPollingReschedulesUntilTerminal(t *testing.T) {
	client := &fakeAgentLLMClient{}
	m := NewApp(client)

	model, _ := m.Update(SendMessageMsg{Content: "/agent implement phase 5"})
	m = model.(AppModel)
	require.Equal(t, 1, client.waitCalls)

	model, _ = m.Update(AgentEventMsg{
		Type:    "turn_start",
		Message: "Turn 1 started",
	})
	m = model.(AppModel)
	require.Equal(t, 2, client.waitCalls, "non-terminal events should keep polling")

	model, _ = m.Update(AgentEventMsg{
		Type:     "run_completed",
		Message:  "Agent run completed",
		Terminal: true,
		Status:   "completed",
	})
	m = model.(AppModel)
	assert.False(t, m.streaming)
	assert.Equal(t, 2, client.waitCalls, "terminal events should stop polling")
}

func hasSystemMessageContaining(messages []ChatMessage, needle string) bool {
	for _, msg := range messages {
		if msg.Role == "system" && strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
