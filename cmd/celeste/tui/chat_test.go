package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLLMMessages_FiltersSystemMessages(t *testing.T) {
	m := NewChatModel()
	m = m.AddUserMessage("hello")
	m = m.AddAssistantMessage("hi there")
	m = m.AddSystemMessage("📂 Resumed session (5 messages)") // UI notification
	m = m.AddUserMessage("how are you?")
	m = m.AddSystemMessage("🔄 Switched to grok") // UI notification

	all := m.GetMessages()
	llm := m.GetLLMMessages()

	assert.Len(t, all, 5, "GetMessages should return all messages including system")
	assert.Len(t, llm, 3, "GetLLMMessages should filter out 2 system messages")

	for _, msg := range llm {
		assert.NotEqual(t, "system", msg.Role, "LLM messages must not contain role=system")
	}
}

func TestGetLLMMessages_EmptyChat(t *testing.T) {
	m := NewChatModel()
	assert.Len(t, m.GetLLMMessages(), 0)
}

func TestGetLLMMessages_OnlySystemMessages(t *testing.T) {
	m := NewChatModel()
	m = m.AddSystemMessage("UI only message 1")
	m = m.AddSystemMessage("UI only message 2")

	llm := m.GetLLMMessages()
	require.Empty(t, llm, "should return empty slice when only system messages exist")
}
