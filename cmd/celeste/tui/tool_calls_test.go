package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

type execCall struct {
	name       string
	args       map[string]any
	toolCallID string
}

type sendCall struct {
	messageCount int
	toolCount    int
}

type fakeToolLLMClient struct {
	skills       []SkillDefinition
	executeCalls []execCall
	sendCalls    []sendCall
}

func (f *fakeToolLLMClient) SendMessage(messages []ChatMessage, tools []SkillDefinition) tea.Cmd {
	f.sendCalls = append(f.sendCalls, sendCall{
		messageCount: len(messages),
		toolCount:    len(tools),
	})
	return nil
}

func (f *fakeToolLLMClient) GetSkills() []SkillDefinition {
	return f.skills
}

func (f *fakeToolLLMClient) ExecuteSkill(name string, args map[string]any, toolCallID string) tea.Cmd {
	f.executeCalls = append(f.executeCalls, execCall{
		name:       name,
		args:       args,
		toolCallID: toolCallID,
	})
	return func() tea.Msg { return nil }
}

func TestSkillCallBatchExecutesSequentiallyAndSendsOneFollowUp(t *testing.T) {
	client := &fakeToolLLMClient{
		skills: []SkillDefinition{
			{Name: "tool_a", Description: "A"},
			{Name: "tool_b", Description: "B"},
		},
	}

	m := NewApp(client)
	m.skillsEnabled = true

	batch := SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{"x": "1"}, Status: "executing"},
				ToolCallID: "call_a",
			},
			{
				Call:       FunctionCall{Name: "tool_b", Arguments: map[string]any{"y": "2"}, Status: "executing"},
				ToolCallID: "call_b",
			},
		},
		AssistantContent: "thinking",
		ToolCalls: []ToolCallInfo{
			{ID: "call_a", Name: "tool_a", Arguments: `{"x":"1"}`},
			{ID: "call_b", Name: "tool_b", Arguments: `{"y":"2"}`},
		},
	}

	model, _ := m.Update(batch)
	m = model.(AppModel)
	require.Len(t, client.executeCalls, 1)
	assert.Equal(t, "tool_a", client.executeCalls[0].name)
	assert.Len(t, client.sendCalls, 0)

	model, _ = m.Update(SkillResultMsg{Name: "tool_a", Result: `{"ok":true}`, ToolCallID: "call_a"})
	m = model.(AppModel)
	require.Len(t, client.executeCalls, 2)
	assert.Equal(t, "tool_b", client.executeCalls[1].name)
	assert.Len(t, client.sendCalls, 0)

	model, _ = m.Update(SkillResultMsg{Name: "tool_b", Result: `{"ok":true}`, ToolCallID: "call_b"})
	m = model.(AppModel)
	require.Len(t, client.sendCalls, 1, "follow-up should be sent once after the final tool result")
	assert.Equal(t, 2, client.sendCalls[0].toolCount)

	messages := m.chat.GetMessages()
	assistantWithCalls := 0
	toolMessages := 0
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) == 2 {
			assistantWithCalls++
		}
		if msg.Role == "tool" {
			toolMessages++
		}
	}
	assert.Equal(t, 1, assistantWithCalls)
	assert.Equal(t, 2, toolMessages)
}

func TestSkillCallBatchParseErrorProducesExplicitToolError(t *testing.T) {
	client := &fakeToolLLMClient{
		skills: []SkillDefinition{
			{Name: "tool_a", Description: "A"},
		},
	}

	m := NewApp(client)
	m.skillsEnabled = true

	model, cmd := m.Update(SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{}, Status: "executing"},
				ToolCallID: "call_bad",
				ParseError: "invalid character '}' looking for beginning of object key string",
			},
		},
		AssistantContent: "thinking",
		ToolCalls: []ToolCallInfo{
			{ID: "call_bad", Name: "tool_a", Arguments: `{"bad":}`},
		},
	})
	m = model.(AppModel)

	require.NotNil(t, cmd)
	parseErrMsg := cmd()

	model, _ = m.Update(parseErrMsg)
	m = model.(AppModel)

	assert.Len(t, client.executeCalls, 0, "parse failure should not call ExecuteSkill")
	require.Len(t, client.sendCalls, 1, "conversation should continue with one follow-up after tool error")
	assert.Equal(t, 1, client.sendCalls[0].toolCount)

	foundError := false
	for _, msg := range m.chat.GetMessages() {
		if msg.Role == "tool" && msg.ToolCallID == "call_bad" {
			foundError = true
			assert.Contains(t, msg.Content, `"error": true`)
		}
	}
	assert.True(t, foundError, "tool error result should be added to chat history")
}

func TestClawModeSafetyStopPreventsInfiniteToolLoops(t *testing.T) {
	client := &fakeToolLLMClient{
		skills: []SkillDefinition{
			{Name: "tool_a", Description: "A"},
		},
	}

	m := NewApp(client)
	m.skillsEnabled = true
	m = m.SetConfig(&config.Config{
		RuntimeMode:           config.RuntimeModeClaw,
		ClawMaxToolIterations: 1,
	})

	model, _ := m.Update(SendMessageMsg{Content: "run tools"})
	m = model.(AppModel)

	firstBatch := SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{"x": "1"}, Status: "executing"},
				ToolCallID: "call_a",
			},
		},
		AssistantContent: "thinking",
		ToolCalls: []ToolCallInfo{
			{ID: "call_a", Name: "tool_a", Arguments: `{"x":"1"}`},
		},
	}

	model, _ = m.Update(firstBatch)
	m = model.(AppModel)
	require.Len(t, client.executeCalls, 1)

	model, _ = m.Update(SkillResultMsg{Name: "tool_a", Result: `{"ok":true}`, ToolCallID: "call_a"})
	m = model.(AppModel)
	require.Len(t, client.sendCalls, 2) // user send + follow-up send

	secondBatch := SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{"x": "2"}, Status: "executing"},
				ToolCallID: "call_b",
			},
		},
		AssistantContent: "thinking again",
		ToolCalls: []ToolCallInfo{
			{ID: "call_b", Name: "tool_a", Arguments: `{"x":"2"}`},
		},
	}
	model, _ = m.Update(secondBatch)
	m = model.(AppModel)

	assert.Len(t, client.executeCalls, 1, "claw safety stop should block additional tool execution")

	foundSafetyStop := false
	for _, msg := range m.chat.GetMessages() {
		if msg.Role == "system" && msg.Content != "" {
			foundSafetyStop = true
			assert.Contains(t, msg.Content, "Claw mode stopped repeated tool calls")
			break
		}
	}
	assert.True(t, foundSafetyStop, "expected claw safety-stop system message")
}

func TestClassicModeIgnoresClawSafetyStop(t *testing.T) {
	client := &fakeToolLLMClient{
		skills: []SkillDefinition{
			{Name: "tool_a", Description: "A"},
		},
	}

	m := NewApp(client)
	m.skillsEnabled = true
	m = m.SetConfig(&config.Config{
		RuntimeMode:           config.RuntimeModeClassic,
		ClawMaxToolIterations: 1,
	})

	model, _ := m.Update(SendMessageMsg{Content: "run tools"})
	m = model.(AppModel)

	firstBatch := SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{"x": "1"}, Status: "executing"},
				ToolCallID: "call_a",
			},
		},
		AssistantContent: "thinking",
		ToolCalls: []ToolCallInfo{
			{ID: "call_a", Name: "tool_a", Arguments: `{"x":"1"}`},
		},
	}
	model, _ = m.Update(firstBatch)
	m = model.(AppModel)
	model, _ = m.Update(SkillResultMsg{Name: "tool_a", Result: `{"ok":true}`, ToolCallID: "call_a"})
	m = model.(AppModel)

	secondBatch := SkillCallBatchMsg{
		Calls: []SkillCallRequest{
			{
				Call:       FunctionCall{Name: "tool_a", Arguments: map[string]any{"x": "2"}, Status: "executing"},
				ToolCallID: "call_b",
			},
		},
		AssistantContent: "thinking again",
		ToolCalls: []ToolCallInfo{
			{ID: "call_b", Name: "tool_a", Arguments: `{"x":"2"}`},
		},
	}
	model, _ = m.Update(secondBatch)
	m = model.(AppModel)

	assert.Len(t, client.executeCalls, 2, "classic mode should continue without claw safety stop")
}
