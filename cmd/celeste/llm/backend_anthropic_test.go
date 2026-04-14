package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestNewAnthropicBackend(t *testing.T) {
	t.Run("requires API key", func(t *testing.T) {
		_, err := NewAnthropicBackend(&Config{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API key is required")
	})

	t.Run("creates backend with API key", func(t *testing.T) {
		backend, err := NewAnthropicBackend(&Config{
			APIKey: "test-key",
			Model:  "claude-sonnet-4-20250514",
		})
		require.NoError(t, err)
		require.NotNil(t, backend)
		assert.NotNil(t, backend.client)
	})
}

func TestAnthropicConvertMessages(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}

	t.Run("converts user message", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{Role: "user", Content: "Hello"},
		})
		require.Len(t, msgs, 1)
		assert.Equal(t, "user", string(msgs[0].Role))
		require.Len(t, msgs[0].Content, 1)
		assert.Equal(t, "Hello", *msgs[0].Content[0].GetText())
	})

	t.Run("converts assistant message", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{Role: "assistant", Content: "Hi there"},
		})
		require.Len(t, msgs, 1)
		assert.Equal(t, "assistant", string(msgs[0].Role))
		require.Len(t, msgs[0].Content, 1)
		assert.Equal(t, "Hi there", *msgs[0].Content[0].GetText())
	})

	t.Run("converts assistant message with tool calls", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{
				Role:    "assistant",
				Content: "Let me check that.",
				ToolCalls: []tui.ToolCallInfo{
					{
						ID:        "call_123",
						Name:      "read_file",
						Arguments: `{"path": "/tmp/test.txt"}`,
					},
				},
			},
		})
		require.Len(t, msgs, 1)
		// Should have text block + tool_use block.
		require.Len(t, msgs[0].Content, 2)
		assert.Equal(t, "Let me check that.", *msgs[0].Content[0].GetText())
		assert.Equal(t, "call_123", *msgs[0].Content[1].GetID())
		assert.Equal(t, "read_file", *msgs[0].Content[1].GetName())
	})

	t.Run("converts tool result message", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{
				Role:       "tool",
				Content:    "file contents here",
				ToolCallID: "call_123",
			},
		})
		require.Len(t, msgs, 1)
		// Tool results go in a user message.
		assert.Equal(t, "user", string(msgs[0].Role))
		require.Len(t, msgs[0].Content, 1)
		assert.Equal(t, "call_123", *msgs[0].Content[0].GetToolUseID())
	})

	t.Run("skips system messages", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
		})
		require.Len(t, msgs, 1)
		assert.Equal(t, "user", string(msgs[0].Role))
	})

	t.Run("skips empty messages", func(t *testing.T) {
		msgs := backend.convertMessages([]tui.ChatMessage{
			{Role: "user", Content: ""},
			{Role: "user", Content: "Hello"},
		})
		require.Len(t, msgs, 1)
	})
}

func TestAnthropicConvertTools(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}

	t.Run("converts basic tool", func(t *testing.T) {
		tools := backend.convertTools([]tui.SkillDefinition{
			{
				Name:        "echo",
				Description: "Echo text",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
					"required": []interface{}{"text"},
				},
			},
		})
		require.Len(t, tools, 1)
		assert.Equal(t, "echo", *tools[0].GetName())
		assert.Equal(t, "Echo text", *tools[0].GetDescription())
	})

	t.Run("handles empty tools list", func(t *testing.T) {
		tools := backend.convertTools([]tui.SkillDefinition{})
		assert.Empty(t, tools)
	})
}

func TestAnthropicBuildSystemBlocks(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}

	t.Run("single block without separator", func(t *testing.T) {
		blocks := backend.buildSystemBlocks("You are a helpful assistant.")
		require.Len(t, blocks, 1)
		assert.Equal(t, "You are a helpful assistant.", blocks[0].Text)
	})

	t.Run("splits on CacheablePrompt separator", func(t *testing.T) {
		prompt := "Static persona content\n\n---\n\nDynamic context content"
		blocks := backend.buildSystemBlocks(prompt)
		require.Len(t, blocks, 2)
		assert.Equal(t, "Static persona content", blocks[0].Text)
		assert.Equal(t, "Dynamic context content", blocks[1].Text)
	})

	t.Run("separator at start produces single block", func(t *testing.T) {
		prompt := "\n\n---\n\nOnly dynamic"
		blocks := backend.buildSystemBlocks(prompt)
		// idx == 0, so no split.
		require.Len(t, blocks, 1)
		assert.Equal(t, prompt, blocks[0].Text)
	})
}

func TestAnthropicSetSystemPrompt(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}
	backend.SetSystemPrompt("You are Celeste.")
	assert.Equal(t, "You are Celeste.", backend.systemPrompt)
}

func TestAnthropicSetThinkingConfig(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}
	backend.SetThinkingConfig(ThinkingConfig{
		Enabled: true,
		Level:   "high",
	})
	assert.True(t, backend.thinkingConfig.Enabled)
	assert.Equal(t, "high", backend.thinkingConfig.Level)
}

func TestAnthropicMaxTokens(t *testing.T) {
	t.Run("default without thinking", func(t *testing.T) {
		// 32768 — raised from 8192 in v1.9.0 so the MCP chat path can
		// return large tool outputs (like code_review JSON dumps) without
		// hitting the Anthropic max_tokens ceiling mid-response.
		backend := &AnthropicBackend{config: &Config{}}
		assert.Equal(t, int64(32768), backend.maxTokens())
	})

	t.Run("increased with thinking", func(t *testing.T) {
		backend := &AnthropicBackend{config: &Config{}}
		backend.thinkingConfig = ThinkingConfig{Enabled: true, Level: "high"}
		// high = 16384 budget + 16384 output room = 32768
		assert.Equal(t, int64(32768), backend.maxTokens())
	})
}

func TestMapStopReason(t *testing.T) {
	assert.Equal(t, "stop", mapStopReason("end_turn"))
	assert.Equal(t, "tool_calls", mapStopReason("tool_use"))
	assert.Equal(t, "length", mapStopReason("max_tokens"))
	assert.Equal(t, "stop", mapStopReason("stop_sequence"))
	assert.Equal(t, "unknown_reason", mapStopReason("unknown_reason"))
}

func TestAnthropicClose(t *testing.T) {
	backend := &AnthropicBackend{config: &Config{}}
	assert.NoError(t, backend.Close())
}

func TestDetectBackendTypeAnthropic(t *testing.T) {
	assert.Equal(t, BackendTypeAnthropic, DetectBackendType("https://api.anthropic.com/v1"))
	assert.Equal(t, BackendTypeAnthropic, DetectBackendType("https://api.anthropic.com"))
}
