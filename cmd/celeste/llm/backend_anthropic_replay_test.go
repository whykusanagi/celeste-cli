package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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

const prefixMismatchBody = "{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"messages.1.content.0: Invalid `signature` in `thinking` block. The block is bound to a different conversation.\"}}"

// historyWithThinking is a finished turn whose blocks b replays, then a new
// user message.
func historyWithThinking(t *testing.T, b *AnthropicBackend) []tui.ChatMessage {
	t.Helper()
	pb, err := tui.NewProviderBlocks(b.providerKey(), []json.RawMessage{
		json.RawMessage(wantThinking),
		json.RawMessage(`{"text":"earlier","type":"text"}`),
	})
	require.NoError(t, err)
	return []tui.ChatMessage{
		{Role: "user", Content: "hi"},
		tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "earlier"}, pb),
		{Role: "user", Content: "again"},
	}
}

func betaHeader(r fakeprovider.Request) string {
	return strings.Join(r.Header.Values("Anthropic-Beta"), ",")
}

// Review Focus 2: the prefix-mismatch 400 is resent once without thinking
// and the reply reports BlocksRejected so the loop strips the history.
func TestAnthropicStripsAndRetriesOnPrefixMismatch(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Status: http.StatusBadRequest, Body: prefixMismatchBody},
		fakeprovider.Turn{Text: "ok"},
	)
	c, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	res, err := c.SendMessageSync(context.Background(), historyWithThinking(t, b), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Content)
	assert.True(t, res.BlocksRejected)
	reqs := srv.Requests()
	require.Len(t, reqs, 2)
	assert.Contains(t, string(reqs[0].Raw), "sig-1")
	assert.NotContains(t, string(reqs[1].Raw), "sig-1")
	assert.Contains(t, string(reqs[1].Raw), `"earlier"`, "the resend keeps the neutral text")
}

// Without replayed blocks, or for another 400, nothing is retried. The
// backend is called directly: the SDK's error text includes the fake's URL,
// and a port such as 55003 would look like a 5xx to llm.Client's retry
// classifier.
func TestAnthropicOtherBadRequestsAreNotRetried(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Status: http.StatusBadRequest, Body: prefixMismatchBody},
		fakeprovider.Turn{Status: http.StatusBadRequest, Body: `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: must be at most 64000"}}`},
	)
	_, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	_, err := b.SendMessageSync(context.Background(), chatMsgs("no blocks here"), nil)
	require.Error(t, err)
	_, err = b.SendMessageSync(context.Background(), historyWithThinking(t, b), nil)
	require.Error(t, err)
	assert.Len(t, srv.Requests(), 2)
}

// Ruling 6: on Anthropic's endpoint a replaying request carries the beta
// and drop_block; always-on models get an explicit adaptive thinking
// parameter for it; requests that replay nothing are unchanged.
func TestAnthropicSendsBindingControlsWhenReplayingThinking(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "a"}, fakeprovider.Turn{Text: "b"}, fakeprovider.Turn{Text: "c"})
	c, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	b.bindingControls = true // the fake is on 127.0.0.1; pretend it is Anthropic's endpoint
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	ctx := context.Background()

	_, err := c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	r := srv.Requests()[0]
	assert.Equal(t, thinkingBindingBeta, betaHeader(r))
	thinking := r.Body["thinking"].(map[string]any)
	assert.Equal(t, "adaptive", thinking["type"])
	assert.Equal(t, map[string]any{"prefix_mismatch_behavior": "drop_block"}, thinking["block_binding"])

	_, err = c.SendMessageSync(ctx, chatMsgs("nothing to replay"), nil)
	require.NoError(t, err)
	r = srv.Requests()[1]
	assert.Empty(t, betaHeader(r))
	assert.NotContains(t, r.Body["thinking"].(map[string]any), "block_binding")

	fable, fb := newAnthropicTestClient(t, srv, "claude-fable-5-1")
	fb.bindingControls = true
	_, err = fable.SendMessageSync(ctx, historyWithThinking(t, fb), nil)
	require.NoError(t, err)
	r = srv.Requests()[2]
	assert.Equal(t, "adaptive", r.Body["thinking"].(map[string]any)["type"])
	assert.Equal(t, thinkingBindingBeta, betaHeader(r))
}

// Review Focus 3: an endpoint that refuses the beta gets the request again
// without it, and never sees the beta again.
func TestAnthropicDropsTheBetaWhenRefused(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t,
		fakeprovider.Turn{Status: http.StatusBadRequest, Body: `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.block_binding: Extra inputs are not permitted"}}`},
		fakeprovider.Turn{Text: "ok"},
		fakeprovider.Turn{Text: "ok again"},
	)
	c, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	b.bindingControls = true
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	ctx := context.Background()

	res, err := c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Content)
	assert.False(t, res.BlocksRejected, "the blocks were accepted; only the beta was not")
	assert.False(t, b.bindingControls)
	_, err = c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	reqs := srv.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, thinkingBindingBeta, betaHeader(reqs[0]))
	for _, r := range reqs[1:] {
		assert.Empty(t, betaHeader(r))
		assert.Contains(t, string(r.Raw), "sig-1", "the blocks still replay")
	}
}

// Review Focus 1 (ruling 10): a system-prompt change strips once, then
// blocks replay again.
func TestAnthropicSystemPromptChangeStripsOnce(t *testing.T) {
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "a"}, fakeprovider.Turn{Text: "b"}, fakeprovider.Turn{Text: "c"})
	c, b := newAnthropicTestClient(t, srv, "claude-opus-4-8")
	ctx := context.Background()

	c.SetSystemPrompt("persona one") // the first prompt is not a change
	res, err := c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	assert.False(t, res.BlocksRejected)

	c.SetSystemPrompt("persona two")
	res, err = c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	assert.True(t, res.BlocksRejected)

	res, err = c.SendMessageSync(ctx, historyWithThinking(t, b), nil)
	require.NoError(t, err)
	assert.False(t, res.BlocksRejected)

	reqs := srv.Requests()
	assert.Contains(t, string(reqs[0].Raw), "sig-1")
	assert.NotContains(t, string(reqs[1].Raw), "sig-1")
	assert.Contains(t, string(reqs[2].Raw), "sig-1")
}
