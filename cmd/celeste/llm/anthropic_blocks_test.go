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
	body, _ := requestBody(t, prepared(t, b, toolLoop(asst)))
	require.Len(t, body.Messages, 3)
	assert.Equal(t, "assistant", body.Messages[1].Role)
	got := body.Messages[1].Content
	require.Len(t, got, 3)
	for i := range got {
		assert.Equal(t, string(asst.ProviderBlocks.Blocks[i]), string(got[i]), "block %d", i)
	}
}

// With binding controls on, prepare adds the beta header and drop_block
// as request options; the replayed bytes in the params are unchanged.
func TestAnthropicPrepareBindsReplayWithoutTouchingBytes(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5-5"}, bindingControls: true}
	asst := thinkingTurn(t, b.providerKey())
	params, opts := b.preparedParams(toolLoop(asst), nil)
	assert.Len(t, opts, 2, "binding beta header and thinking.block_binding")
	require.NotNil(t, params.Thinking.OfAdaptive, "always-on model: adaptive thinking carries block_binding")
	body, _ := requestBody(t, params)
	require.Len(t, body.Messages, 3)
	for i, blk := range body.Messages[1].Content {
		assert.Equal(t, string(asst.ProviderBlocks.Blocks[i]), string(blk), "block %d", i)
	}
}

// Replayed blocks never get cache_control; the breakpoints land on the
// tool result and the first user message.
func TestAnthropicReplayedBlocksCarryNoCacheControl(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-4-8"}}
	body, raw := requestBody(t, prepared(t, b, toolLoop(thinkingTurn(t, b.providerKey()))))
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

	body, raw := requestBody(t, prepared(t, haiku, history))
	assert.NotContains(t, raw, "sig-1")
	assert.Empty(t, body.Thinking, "budget model, continuation without replay: thinking off")

	_, raw = requestBody(t, prepared(t, opus, history))
	assert.Contains(t, raw, "sig-1")
}

func TestAnthropicRejectionClassifiers(t *testing.T) {
	mismatch := strings.ToLower("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"messages.1.content.0: Invalid `signature` in `thinking` block. The block is bound to a different conversation. Remove the block, or set `thinking.block_binding.prefix_mismatch_behavior` to drop_block. That setting requires the `thinking-binding-controls-2026-08-01` value in the `anthropic-beta` header.\"}}")
	beta := strings.ToLower(`{"type":"error","error":{"type":"invalid_request_error","message":"thinking.block_binding: Extra inputs are not permitted"}}`)
	unknownBeta := strings.ToLower(`{"type":"error","error":{"type":"invalid_request_error","message":"Unexpected value(s) thinking-binding-controls-2026-08-01 for the anthropic-beta header"}}`)
	other := strings.ToLower(`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: must be at most 64000"}}`)

	assert.True(t, isThinkingRejection(mismatch))
	assert.False(t, isThinkingRejection(beta), "thinking.block_binding is not a thinking block")
	assert.False(t, isThinkingRejection(other))
	assert.True(t, isBindingBetaRejection(beta))
	assert.True(t, isBindingBetaRejection(unknownBeta))
	assert.False(t, isBindingBetaRejection(other))
}

func TestAnthropicBindingControlsOnlyOnAnthropicsEndpoint(t *testing.T) {
	for base, want := range map[string]bool{
		"":                             true,
		"https://api.anthropic.com":    true,
		"https://api.anthropic.com/v1": true,
		"http://127.0.0.1:9999":        false,
		"https://proxy.example.test":   false,
	} {
		b, err := NewAnthropicBackend(&Config{APIKey: "k", BaseURL: base, Model: "claude-opus-4-8"})
		require.NoError(t, err)
		assert.Equal(t, want, b.bindingControls, base)
	}
}
