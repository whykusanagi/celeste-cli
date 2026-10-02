package llm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const (
	wantThinking = `{"signature":"sig-1","thinking":"plan","type":"thinking"}`
	wantRedacted = `{"data":"opaque","type":"redacted_thinking"}`
)

func newAnthropicTestClient(t *testing.T, srv *fakeprovider.Server, model string) (*Client, *AnthropicBackend) {
	t.Helper()
	cfg := &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: model, Timeout: 10 * time.Second}
	b, err := NewAnthropicBackend(cfg)
	require.NoError(t, err)
	return NewClientWithBackend(cfg, nil, b), b
}

func thinkingReply(text string) fakeprovider.Turn {
	return fakeprovider.Turn{Thinking: &fakeprovider.Thinking{Text: "plan", Signature: "sig-1"}, RedactedThinking: "opaque", Text: text}
}

func chatMsgs(s string) []tui.ChatMessage { return []tui.ChatMessage{{Role: "user", Content: s}} }

func listFilesSkill() []tui.SkillDefinition {
	return []tui.SkillDefinition{{Name: "list_files", Description: "List files",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"dir": map[string]any{"type": "string"}}}}}
}

// Spec §5 W2 "Capture": all three stream paths keep thinking and
// redacted_thinking with signatures, in order with the text.
func TestAnthropicKeepsThinkingOnAllThreePaths(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, thinkingReply("sync"), thinkingReply("stream"), thinkingReply("events"))
	c, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	check := func(path string, pb *tui.ProviderBlocks, text string) {
		t.Helper()
		require.NotNil(t, pb, path)
		assert.Equal(t, b.providerKey(), pb.Provider, path)
		require.Len(t, pb.Blocks, 3, path)
		assert.Equal(t, wantThinking, string(pb.Blocks[0]), path)
		assert.Equal(t, wantRedacted, string(pb.Blocks[1]), path)
		assert.Equal(t, `{"text":"`+text+`","type":"text"}`, string(pb.Blocks[2]), path)
	}
	ctx := context.Background()

	res, err := c.SendMessageSync(ctx, chatMsgs("hi"), nil)
	require.NoError(t, err)
	check("sync", res.ProviderBlocks, "sync")
	assert.False(t, res.BlocksRejected)

	var final StreamChunk
	err = c.SendMessageStream(ctx, chatMsgs("hi"), nil, func(ch StreamChunk) {
		if ch.IsFinal && final.ProviderBlocks == nil {
			final = ch
		}
	})
	require.NoError(t, err)
	check("stream", final.ProviderBlocks, "stream")

	var done StreamEvent
	err = c.SendMessageStreamEvents(ctx, chatMsgs("hi"), nil, func(ev StreamEvent) {
		if ev.Type == EventMessageDone {
			done = ev
		}
	})
	require.NoError(t, err)
	check("events", done.ProviderBlocks, "events")
}

// Ruling 2: a reply without thinking keeps nothing (its neutral view is
// the same content).
func TestAnthropicKeepsNoBlocksWithoutThinking(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "plain"})
	c, _ := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	res, err := c.SendMessageSync(context.Background(), chatMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "plain", res.Content)
	assert.Nil(t, res.ProviderBlocks)
}

// The captured blocks go back on the wire byte for byte, and a budget
// model keeps thinking on the continuation (ruling 5).
func TestAnthropicReplaysCapturedBlocksOnTheWire(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Thinking: &fakeprovider.Thinking{Text: "plan", Signature: "sig-1"},
			ToolCalls: []fakeprovider.ToolCall{{ID: "toolu_1", Name: "list_files", Args: `{"dir":"."}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	c, _ := newAnthropicTestClient(t, srv, "claude-haiku-4-5")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	ctx := context.Background()

	history := chatMsgs("list")
	res, err := c.SendMessageSync(ctx, history, listFilesSkill())
	require.NoError(t, err)
	require.Len(t, res.ToolCalls, 1)
	require.NotNil(t, res.ProviderBlocks)
	assert.JSONEq(t, `{"id":"toolu_1","input":{"dir":"."},"name":"list_files","type":"tool_use"}`, string(res.ProviderBlocks.Blocks[1]))
	tc := res.ToolCalls[0]
	asst := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant",
		ToolCalls: []tui.ToolCallInfo{{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}}}, res.ProviderBlocks)
	history = append(history, asst, tui.ChatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Name, Content: "a.go"})

	_, err = c.SendMessageSync(ctx, history, listFilesSkill())
	require.NoError(t, err)
	var body anthropicRequestBody
	require.NoError(t, json.Unmarshal(srv.Requests()[1].Raw, &body))
	got := body.Messages[1].Content
	require.Len(t, got, 2)
	for i := range got {
		assert.Equal(t, string(res.ProviderBlocks.Blocks[i]), string(got[i]), "block %d", i)
	}
	assert.Contains(t, string(body.Thinking), "budget_tokens")
}

// Ruling 7: a prefix_binding_mismatch drop reports BlocksRejected; other
// entries are only logged.
func TestAnthropicReportsPrefixDrops(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Text: "a", Transformations: `[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]`},
		fakeprovider.Turn{Text: "b", Transformations: `[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"model_binding_mismatch"},{"type":"something_new"}]`},
		fakeprovider.Turn{Text: "c", Transformations: `[]`},
	)
	c, _ := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	for _, want := range []bool{true, false, false} {
		res, err := c.SendMessageSync(context.Background(), chatMsgs("hi"), nil)
		require.NoError(t, err)
		assert.Equal(t, want, res.BlocksRejected, res.Content)
	}
}
