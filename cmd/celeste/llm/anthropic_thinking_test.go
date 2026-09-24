package llm

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// requestJSON builds the request for model and returns its thinking and
// output_config fields as JSON (empty string when absent).
func requestJSON(t *testing.T, model string, tc ThinkingConfig, msgs []tui.ChatMessage) (thinking, outputConfig string, maxTokens int64) {
	t.Helper()
	b := &AnthropicBackend{config: &Config{Model: model}, thinkingConfig: tc}
	params := b.buildParams(msgs, nil)
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &body))
	return string(body["thinking"]), string(body["output_config"]), params.MaxTokens
}

var userTurn = []tui.ChatMessage{{Role: "user", Content: "hi"}}

// Current models reject budget_tokens; each family gets the parameter it
// accepts (#189).
func TestAnthropicThinkingParamPerModel(t *testing.T) {
	on := ThinkingConfig{Enabled: true, Level: "high"}
	off := ThinkingConfig{}

	cases := []struct {
		model        string
		tc           ThinkingConfig
		wantThinking string // substring; "" means the field must be absent
		wantEffort   string
	}{
		// Always-on models: no thinking parameter at all.
		{"claude-fable-5-1", on, "", `"high"`},
		{"claude-fable-5-1", off, "", ""},
		{"claude-opus-5-5", on, "", `"high"`},
		// Opus 5 / Sonnet 5 think by default: adaptive when on, disabled when off.
		{"claude-opus-5", on, `"adaptive"`, `"high"`},
		{"claude-opus-5", off, `"disabled"`, ""},
		{"claude-sonnet-5", on, `"adaptive"`, `"high"`},
		// 4.6-4.8: adaptive when on, omitted when off.
		{"claude-opus-4-8", on, `"adaptive"`, `"high"`},
		{"claude-sonnet-4-6", on, `"adaptive"`, `"high"`},
		{"claude-opus-4-7", off, "", ""},
		// Older models keep budget_tokens.
		{"claude-haiku-4-5", on, `"budget_tokens"`, ""},
		{"claude-sonnet-4-20250514", on, `"budget_tokens"`, ""},
		{"claude-haiku-4-5", off, "", ""},
	}
	for _, c := range cases {
		thinking, oc, _ := requestJSON(t, c.model, c.tc, userTurn)
		if c.wantThinking == "" {
			assert.Empty(t, thinking, "%s: thinking should be omitted", c.model)
		} else {
			assert.Contains(t, thinking, c.wantThinking, "%s", c.model)
		}
		if c.wantEffort == "" {
			assert.NotContains(t, oc, "effort", "%s", c.model)
		} else {
			assert.Contains(t, oc, c.wantEffort, "%s", c.model)
		}
	}
}

// Budget-thinking models run tool-loop continuations without thinking, since
// thinking blocks aren't replayed; adaptive models keep thinking.
func TestAnthropicThinkingOnToolContinuation(t *testing.T) {
	on := ThinkingConfig{Enabled: true, Level: "high"}
	loop := []tui.ChatMessage{
		{Role: "user", Content: "list files"},
		{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "t1", Name: "list_files", Arguments: "{}"}}},
		{Role: "tool", ToolCallID: "t1", Name: "list_files", Content: "a.go"},
	}
	thinking, _, _ := requestJSON(t, "claude-haiku-4-5", on, loop)
	assert.Empty(t, thinking, "budget thinking must be off on a tool continuation")

	thinking, _, _ = requestJSON(t, "claude-opus-4-8", on, loop)
	assert.Contains(t, thinking, `"adaptive"`)
}

// Models that think by default get room for thinking in max_tokens.
func TestAnthropicMaxTokensForThinkingModels(t *testing.T) {
	_, _, mt := requestJSON(t, "claude-fable-5-1", ThinkingConfig{}, userTurn)
	assert.Equal(t, int64(65536), mt)
	_, _, mt = requestJSON(t, "claude-opus-5", ThinkingConfig{}, userTurn)
	assert.Equal(t, int64(32768), mt, "thinking disabled: normal ceiling")
}

func TestAnthropicThinkingFamily(t *testing.T) {
	assert.Equal(t, familyAlwaysOn, anthropicThinkingFamily("anthropic.claude-fable-5"))
	assert.Equal(t, familyAlwaysOn, anthropicThinkingFamily("claude-opus-5-5"))
	assert.Equal(t, familyAdaptiveDefaultOn, anthropicThinkingFamily("claude-opus-5"))
	assert.Equal(t, familyAdaptive, anthropicThinkingFamily("claude-opus-4-6"))
	assert.Equal(t, familyBudget, anthropicThinkingFamily("claude-opus-4-5"))
	assert.Equal(t, familyBudget, anthropicThinkingFamily("claude-3-7-sonnet-latest"))
}

// message_delta must not zero the prompt tokens, and cache reads/writes count
// toward the prompt (#189).
func TestUsageTracker(t *testing.T) {
	var u usageTracker
	assert.Nil(t, u.result())

	u.start(anthropic.Usage{InputTokens: 12, CacheReadInputTokens: 9000, CacheCreationInputTokens: 300, OutputTokens: 1})
	u.delta(anthropic.MessageDeltaUsage{OutputTokens: 250}) // input fields zero, as the API sends them

	got := u.result()
	require.NotNil(t, got)
	assert.Equal(t, 9312, got.PromptTokens)
	assert.Equal(t, 250, got.CompletionTokens)
	assert.Equal(t, 9562, got.TotalTokens)
	assert.Equal(t, 9000, got.CacheReadTokens)
	assert.Equal(t, 300, got.CacheWriteTokens)
}
