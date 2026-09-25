package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// The request carries rolling cache breakpoints on the newest two messages
// and the tool list, plus the system prompt's: four in all, the API's
// maximum (#174).
func TestAnthropicRollingCacheBreakpoints(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}, systemPrompt: "persona\n\n---\n\ntoday"}
	msgs := []tui.ChatMessage{
		{Role: "user", Content: "read a and b"},
		{Role: "assistant", Content: "reading", ToolCalls: []tui.ToolCallInfo{
			{ID: "c1", Name: "read_file", Arguments: `{"path":"a"}`},
			{ID: "c2", Name: "read_file", Arguments: `{"path":"b"}`},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "A"},
		{Role: "tool", ToolCallID: "c2", Content: "B"},
	}
	tools := []tui.SkillDefinition{{Name: "read_file"}, {Name: "bash"}}
	params := b.buildParams(msgs, tools)

	raw, err := json.Marshal(params)
	require.NoError(t, err)
	assert.Equal(t, 4, strings.Count(string(raw), `"cache_control"`), "system + tools + two messages")

	cached := func(v any) bool {
		j, err := json.Marshal(v)
		require.NoError(t, err)
		return strings.Contains(string(j), `"cache_control"`)
	}
	assert.True(t, cached(params.Tools[1]), "the last tool is a breakpoint")
	assert.False(t, cached(params.Tools[0]))
	n := len(params.Messages)
	assert.True(t, cached(params.Messages[n-1]), "the last message is a breakpoint")
	assert.True(t, cached(params.Messages[n-2]), "so is the one before")
	for i := 0; i < n-2; i++ {
		assert.False(t, cached(params.Messages[i]), "message %d", i)
	}
}

// A request with no tools and a single message stays within the limit.
func TestAnthropicCacheBreakpointsShortRequest(t *testing.T) {
	b := &AnthropicBackend{config: &Config{Model: "claude-opus-5"}}
	params := b.buildParams([]tui.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(raw), `"cache_control"`))
}
