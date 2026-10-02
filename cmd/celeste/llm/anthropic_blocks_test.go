package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// thinkingTurn is an assistant turn that called list_files (t1) after
// thinking, with its blocks stored under key.
func thinkingTurn(t *testing.T, key string) tui.ChatMessage {
	t.Helper()
	pb, err := tui.NewProviderBlocks(key, []json.RawMessage{
		json.RawMessage(`{"signature":"sig-1","thinking":"plan","type":"thinking"}`),
		json.RawMessage(`{"data":"opaque","type":"redacted_thinking"}`),
		json.RawMessage(`{"id":"t1","input":{},"name":"list_files","type":"tool_use"}`),
	})
	require.NoError(t, err)
	return tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant",
		ToolCalls: []tui.ToolCallInfo{{ID: "t1", Name: "list_files", Arguments: "{}"}}}, pb)
}

func toolLoop(asst tui.ChatMessage) []tui.ChatMessage {
	return []tui.ChatMessage{
		{Role: "user", Content: "list files"},
		asst,
		{Role: "tool", ToolCallID: "t1", Name: "list_files", Content: "a.go"},
	}
}

type anthropicRequestBody struct {
	Thinking json.RawMessage `json:"thinking"`
	Messages []struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	} `json:"messages"`
}

func requestBody(t *testing.T, params anthropic.MessageNewParams) (anthropicRequestBody, string) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	var body anthropicRequestBody
	require.NoError(t, json.Unmarshal(raw, &body))
	return body, string(raw)
}

func TestAnthropicProviderKey(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	assert.Equal(t, "anthropic-messages|https://api.anthropic.com|claude-opus-4-8", b.providerKey())
	b.config.BaseURL = "https://API.anthropic.com/"
	assert.Equal(t, "anthropic-messages|https://api.anthropic.com|claude-opus-4-8", b.providerKey())
}

// Ruling 3: the request carries the stored bytes, in order.
func TestAnthropicReplaysStoredBlocksByteForByte(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	asst := thinkingTurn(t, b.providerKey())
	body, _ := requestBody(t, b.buildParams(toolLoop(asst), nil))
	require.Len(t, body.Messages, 3)
	assert.Equal(t, "assistant", body.Messages[1].Role)
	got := body.Messages[1].Content
	require.Len(t, got, 3)
	for i := range got {
		assert.Equal(t, string(asst.ProviderBlocks.Blocks[i]), string(got[i]), "block %d", i)
	}
}

// Replayed blocks never get cache_control; the breakpoints land on the
// tool result and the first user message.
func TestAnthropicReplayedBlocksCarryNoCacheControl(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	body, raw := requestBody(t, b.buildParams(toolLoop(thinkingTurn(t, b.providerKey())), nil))
	for _, blk := range body.Messages[1].Content {
		assert.NotContains(t, string(blk), "cache_control")
	}
	assert.Equal(t, 2, strings.Count(raw, `"cache_control"`))
}

// A blocks-only message (W1's compaction carrier) is sent, not skipped.
func TestAnthropicReplaysABlocksOnlyMessage(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	pb, err := tui.NewProviderBlocks(b.providerKey(), []json.RawMessage{json.RawMessage(`{"content":"summary","type":"compaction"}`)})
	require.NoError(t, err)
	carrier := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant"}, pb)
	msgs := b.convertMessages([]tui.ChatMessage{{Role: "user", Content: "hi"}, carrier, {Role: "user", Content: "go on"}})
	require.Len(t, msgs, 3)
	assert.Equal(t, anthropic.MessageParamRoleAssistant, msgs[1].Role)
}

// Review Focus 5: another model's blocks are never sent; switching back
// replays them again.
func TestAnthropicReplayOnlyForTheSameKey(t *testing.T) {
	on := ThinkingConfig{Enabled: true, Level: "high"}
	opus := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}, thinkingConfig: on}
	haiku := &AnthropicBackend{config: &Config{Model: "claude-haiku-4-5"}, thinkingConfig: on}
	history := toolLoop(thinkingTurn(t, opus.providerKey()))

	body, raw := requestBody(t, haiku.buildParams(history, nil))
	assert.NotContains(t, raw, "sig-1")
	assert.Empty(t, body.Thinking, "budget model, continuation without replay: thinking off")

	_, raw = requestBody(t, opus.buildParams(history, nil))
	assert.Contains(t, raw, "sig-1")
}
