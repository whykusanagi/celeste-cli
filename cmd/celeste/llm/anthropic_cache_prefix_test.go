package llm

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// wireParts is a request body split the way Anthropic's prompt cache sees
// it: tools, then system, then messages, each as the bytes sent.
type wireParts struct {
	Tools    json.RawMessage   `json:"tools"`
	System   json.RawMessage   `json:"system"`
	Messages []json.RawMessage `json:"messages"`
	Thinking json.RawMessage   `json:"thinking"`
}

func splitWire(t *testing.T, raw []byte) wireParts {
	t.Helper()
	var p wireParts
	require.NoError(t, json.Unmarshal(raw, &p))
	return p
}

// cacheControl matches a cache_control marker. The breakpoints roll forward
// with the conversation by design; they mark the prefix, they are not part
// of it.
var cacheControl = regexp.MustCompile(`,?"cache_control":\{"type":"ephemeral"\}`)

func withoutBreakpoints(raw json.RawMessage) string {
	return cacheControl.ReplaceAllString(string(raw), "")
}

// assertPrefixKept checks that next starts with prev's cacheable prefix:
// the same tools and system bytes, and prev's messages unchanged.
func assertPrefixKept(t *testing.T, prev, next wireParts, label string) {
	t.Helper()
	assert.Equal(t, string(prev.Tools), string(next.Tools), "%s: tools changed", label)
	assert.Equal(t, string(prev.System), string(next.System), "%s: system changed", label)
	require.GreaterOrEqual(t, len(next.Messages), len(prev.Messages), label)
	for i := range prev.Messages {
		assert.Equal(t, withoutBreakpoints(prev.Messages[i]), withoutBreakpoints(next.Messages[i]), "%s: message %d changed", label, i)
	}
}

// #318: on a budget-thinking model (Sonnet 4.5, the default Anthropic
// model) the second continuation of one tool turn went out with thinking
// off, because the assistant message it continued had no thinking block of
// its own (without interleaved thinking the model thinks once, at the start
// of the turn). Thinking flipping off and back on re-wrote the cached
// prefix twice. The thinking parameter must hold for the whole tool turn
// and every request must extend the previous one's prefix.
func TestAnthropicBudgetThinkingHoldsForTheWholeToolTurn(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Thinking: &fakeprovider.Thinking{Text: "plan", Signature: "sig-1"},
			ToolCalls: []fakeprovider.ToolCall{{ID: "toolu_1", Name: "list_files", Args: `{"dir":"."}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "toolu_2", Name: "list_files", Args: `{"dir":"cmd"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	c, _ := newAnthropicTestClient(t, srv, "claude-sonnet-4-5-20250929")
	c.SetSystemPrompt("persona\n\n---\n\ntoday")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	ctx := context.Background()

	history := chatMsgs("list everything")
	for i, out := range []string{"a.go", "main.go"} {
		res, err := c.SendMessageSync(ctx, history, listFilesSkill())
		require.NoError(t, err)
		require.Len(t, res.ToolCalls, 1, "turn %d", i)
		tc := res.ToolCalls[0]
		asst := tui.ChatMessage{Role: "assistant",
			ToolCalls: []tui.ToolCallInfo{{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}}}
		if res.ProviderBlocks != nil {
			asst = tui.AttachProviderBlocks(asst, res.ProviderBlocks)
		}
		history = append(history, asst, tui.ChatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Name, Content: out})
	}
	_, err := c.SendMessageSync(ctx, history, listFilesSkill())
	require.NoError(t, err)

	reqs := srv.Requests()
	require.Len(t, reqs, 3)
	parts := make([]wireParts, len(reqs))
	for i, r := range reqs {
		parts[i] = splitWire(t, r.Raw)
		assert.Contains(t, string(parts[i].Thinking), `"budget_tokens"`, "request %d: thinking must stay on through the tool turn", i)
		assert.Equal(t, string(parts[0].Thinking), string(parts[i].Thinking), "request %d: thinking parameter changed mid-turn", i)
	}
	for i := 1; i < len(parts); i++ {
		assertPrefixKept(t, parts[i-1], parts[i], "request "+string(rune('0'+i)))
	}
}

// #318: toggling /effort changes only the thinking parameters. Celeste
// must not touch the tools, the system prompt or the history on a toggle;
// whatever cache cost remains is the API's own rule (thinking settings are
// part of the cache key), documented in docs/LLM_PROVIDERS.md.
func TestAnthropicEffortToggleKeepsToolsSystemAndMessagesBytes(t *testing.T) {
	levels := []ThinkingConfig{{}, {Enabled: true, Level: "medium"}, {Enabled: true, Level: "high"}, {Enabled: false, Level: "off"}}
	for _, model := range []string{"claude-sonnet-4-5-20250929", "claude-sonnet-4-6", "claude-opus-5", "claude-opus-5-5", "claude-fable-5-1"} {
		srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "a"}, fakeprovider.Turn{Text: "b"}, fakeprovider.Turn{Text: "c"}, fakeprovider.Turn{Text: "d"})
		c, b := newAnthropicTestClient(t, srv, model)
		c.SetSystemPrompt("persona\n\n---\n\ntoday")
		history := historyWithThinking(t, b)
		for _, tc := range levels {
			c.SetThinkingConfig(tc)
			_, err := c.SendMessageSync(context.Background(), history, listFilesSkill())
			require.NoError(t, err, model)
		}
		reqs := srv.Requests()
		require.Len(t, reqs, len(levels), model)
		first := splitWire(t, reqs[0].Raw)
		for i := 1; i < len(reqs); i++ {
			next := splitWire(t, reqs[i].Raw)
			assert.Equal(t, string(first.Tools), string(next.Tools), "%s level %d: tools", model, i)
			assert.Equal(t, string(first.System), string(next.System), "%s level %d: system", model, i)
			require.Equal(t, len(first.Messages), len(next.Messages), model)
			for j := range first.Messages {
				assert.Equal(t, string(first.Messages[j]), string(next.Messages[j]), "%s level %d: message %d", model, i, j)
			}
		}
	}
}

// The turn's opening assistant message decides: when it does not carry a
// thinking block (thinking was off when the turn started, or its blocks were
// stripped), a budget model keeps thinking off for the rest of the turn even
// if a later message in the turn replays blocks.
func TestAnthropicBudgetThinkingFollowsTheTurnOpener(t *testing.T) {
	on := ThinkingConfig{Enabled: true, Level: "high"}
	key := (&AnthropicBackend{config: &Config{Model: "claude-haiku-4-5"}}).providerKey()
	opener := thinkingTurn(t, key)
	second := tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "t2", Name: "list_files", Arguments: "{}"}}}
	turn := append(toolLoop(opener), second, tui.ChatMessage{Role: "tool", ToolCallID: "t2", Name: "list_files", Content: "b.go"})

	thinking, _, _ := requestJSON(t, "claude-haiku-4-5", on, turn)
	assert.Contains(t, thinking, `"budget_tokens"`, "opener replays its thinking: thinking stays on")

	thinking, _, _ = requestJSON(t, "claude-haiku-4-5", on, tui.StripProviderBlocks(turn))
	assert.Empty(t, thinking, "opener stripped: thinking off")

	// A previous turn's thinking does not count for this one.
	prev := []tui.ChatMessage{{Role: "user", Content: "earlier"}, opener, {Role: "tool", ToolCallID: "t1", Name: "list_files", Content: "a.go"}, {Role: "assistant", Content: "ok"}}
	plain := toolLoop(tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: "t1", Name: "list_files", Arguments: "{}"}}})
	thinking, _, _ = requestJSON(t, "claude-haiku-4-5", on, append(prev, plain...))
	assert.Empty(t, thinking, "this turn's opener has no thinking: thinking off")
}
