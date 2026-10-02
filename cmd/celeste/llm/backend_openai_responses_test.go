package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func newResponsesTestClient(t *testing.T, srv *fakeprovider.Server, model string) (*Client, *ResponsesBackend) {
	t.Helper()
	resetResponsesFallback()
	t.Cleanup(resetResponsesFallback)
	cfg := &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: model, Timeout: 10 * time.Second}
	b := NewResponsesBackend(cfg)
	return NewClientWithBackend(cfg, nil, b), b
}

func readFileSkill() []tui.SkillDefinition {
	return []tui.SkillDefinition{{Name: "read_file", Description: "Read a file",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}}}
}

func userMsgs(s string) []tui.ChatMessage { return []tui.ChatMessage{{Role: "user", Content: s}} }

func TestResponsesStreamEventsTextCallsAndBlocks(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{
		Reasoning: &fakeprovider.Reasoning{ID: "rs_1", Summary: "plan", Encrypted: "enc-1"},
		Text:      "reading",
		ToolCalls: []fakeprovider.ToolCall{{ID: "call_a", Name: "read_file", Args: `{"path":"a.go"}`}},
	})
	c, b := newResponsesTestClient(t, srv, "gpt-test")
	c.SetSystemPrompt("be brief")

	var text string
	var done StreamEvent
	acc := NewToolUseAccumulator()
	err := c.SendMessageStreamEvents(context.Background(), userMsgs("read a.go"), readFileSkill(), func(ev StreamEvent) {
		acc.HandleEvent(ev)
		switch ev.Type {
		case EventContentDelta:
			text += ev.ContentDelta
		case EventMessageDone:
			done = ev
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "reading", text)
	calls := acc.CompletedCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "call_a", calls[0].ID, "tool calls are keyed by call_id, not the fc_ item id")
	assert.Equal(t, `{"path":"a.go"}`, calls[0].Arguments)
	assert.Equal(t, "tool_calls", done.FinishReason)
	require.NotNil(t, done.Usage)
	assert.Equal(t, 100, done.Usage.PromptTokens)
	assert.Equal(t, 40, done.Usage.CacheReadTokens)
	assert.Equal(t, 110, done.Usage.TotalTokens)
	require.NotNil(t, done.ProviderBlocks)
	assert.Equal(t, b.providerKey(), done.ProviderBlocks.Provider)
	assert.Equal(t, "openai-responses|"+strings.ToLower(srv.BaseURL())+"|gpt-test", b.providerKey())
	require.Len(t, done.ProviderBlocks.Blocks, 3)
	assert.JSONEq(t, `{"id":"rs_1","type":"reasoning","encrypted_content":"enc-1","summary":[{"type":"summary_text","text":"plan"}]}`, string(done.ProviderBlocks.Blocks[0]))
	assert.Contains(t, string(done.ProviderBlocks.Blocks[1]), `"id":"msg_1"`)
	assert.Contains(t, string(done.ProviderBlocks.Blocks[2]), `"call_id":"call_a"`)

	req := srv.Requests()[0]
	assert.Equal(t, "/v1/responses", req.Path)
	assert.Equal(t, "gpt-test", req.Body["model"])
	assert.Equal(t, false, req.Body["store"])
	assert.Equal(t, []any{"reasoning.encrypted_content"}, req.Body["include"])
	assert.Equal(t, "be brief", req.Body["instructions"])
	assert.Equal(t, true, req.Body["stream"])
	assert.NotContains(t, req.Body, "previous_response_id")
	assert.NotContains(t, req.Body, "reasoning", "gpt-test is not a reasoning model")
	tool := req.Body["tools"].([]any)[0].(map[string]any)
	assert.Equal(t, "function", tool["type"])
	assert.Equal(t, "read_file", tool["name"])
	assert.Equal(t, false, tool["strict"])
}

func TestResponsesSyncAndStreamPathsCarryBlocks(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Reasoning: &fakeprovider.Reasoning{ID: "rs_1", Summary: "s", Encrypted: "e"}, Text: "sync"},
		fakeprovider.Turn{Text: "streamed"},
	)
	c, _ := newResponsesTestClient(t, srv, "gpt-test")

	res, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "sync", res.Content)
	assert.Equal(t, "stop", res.FinishReason)
	require.NotNil(t, res.ProviderBlocks)
	assert.Len(t, res.ProviderBlocks.Blocks, 2)
	assert.False(t, res.BlocksRejected)

	var text string
	var final StreamChunk
	err = c.SendMessageStream(context.Background(), userMsgs("hi"), nil, func(ch StreamChunk) {
		text += ch.Content
		if ch.IsFinal {
			final = ch
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "streamed", text)
	assert.Equal(t, "stop", final.FinishReason)
	require.NotNil(t, final.ProviderBlocks)
	assert.Len(t, final.ProviderBlocks.Blocks, 1)
}

// The next request sends the recorded items in order: the reasoning item
// byte for byte, the function_call without its id, then the tool result.
func TestResponsesReplaysOutputItems(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{
			Reasoning: &fakeprovider.Reasoning{ID: "rs_1", Summary: "plan", Encrypted: "enc-1"},
			ToolCalls: []fakeprovider.ToolCall{{ID: "call_a", Name: "read_file", Args: `{"path":"a.go"}`}},
		},
		fakeprovider.Turn{Text: "done"},
	)
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	msgs := userMsgs("read a.go")
	res, err := c.SendMessageSync(context.Background(), msgs, readFileSkill())
	require.NoError(t, err)
	require.Len(t, res.ToolCalls, 1)
	tc := res.ToolCalls[0]
	asst := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant",
		ToolCalls: []tui.ToolCallInfo{{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}}}, res.ProviderBlocks)
	msgs = append(msgs, asst, tui.ChatMessage{Role: "tool", ToolCallID: "call_a", Name: "read_file", Content: "package a"})

	_, err = c.SendMessageSync(context.Background(), msgs, readFileSkill())
	require.NoError(t, err)
	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(srv.Requests()[1].Raw, &body))
	require.Len(t, body.Input, 4)
	assert.Equal(t, string(res.ProviderBlocks.Blocks[0]), string(body.Input[1]), "reasoning item must be replayed byte for byte")
	assert.JSONEq(t, `{"arguments":"{\"path\":\"a.go\"}","call_id":"call_a","name":"read_file","status":"completed","type":"function_call"}`, string(body.Input[2]))
	assert.Equal(t, `{"type":"function_call_output","call_id":"call_a","output":"package a"}`, string(body.Input[3]))
}

func TestResponsesReasoningEffortForReasoningModels(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Text: "ok"})
	c, _ := newResponsesTestClient(t, srv, "gpt-5")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "max"})
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"effort": "high"}, srv.Requests()[0].Body["reasoning"])
}

// Review Focus 3: a reply cut off before its terminal event is an error and
// carries no blocks.
func TestResponsesCutStreamIsAnError(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Text: "partial", Truncate: true})
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	sawDone := false
	err := c.SendMessageStreamEvents(context.Background(), userMsgs("hi"), nil, func(ev StreamEvent) {
		if ev.Type == EventMessageDone {
			sawDone = true
		}
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed before the response finished")
	assert.False(t, sawDone)
}

func TestResponsesFailedResponseIsAnError(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Fail: "boom"})
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, 1, len(srv.Requests()), "a failed response is not retried")
}

func TestResponsesIncompleteIsALengthStop(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Text: "cut", Incomplete: "max_output_tokens"})
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	res, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "cut", res.Content)
	assert.Equal(t, "length", res.FinishReason)
}

// scriptedEvents feeds readResponses decoded events, then io.EOF.
type scriptedEvents []string

func (s *scriptedEvents) Recv() (openai.ResponseStreamEvent, error) {
	var ev openai.ResponseStreamEvent
	if len(*s) == 0 {
		return ev, io.EOF
	}
	raw := (*s)[0]
	*s = (*s)[1:]
	err := json.Unmarshal([]byte(raw), &ev)
	return ev, err
}

// Ruling 9: an error event ends the reply with the server's code and
// message; a function_call that arrives only as output_item.done still
// becomes a call keyed by call_id.
func TestReadResponsesErrorEventAndDoneOnlyCall(t *testing.T) {
	errs := scriptedEvents{`{"type":"error","code":"rate_limit_exceeded","message":"slow down"}`}
	_, err := readResponses(&errs, func(StreamEvent) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit_exceeded: slow down")

	var evs []StreamEvent
	done := scriptedEvents{
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_9","type":"function_call","call_id":"call_z","name":"read_file","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`,
	}
	turn, err := readResponses(&done, func(ev StreamEvent) { evs = append(evs, ev) })
	require.NoError(t, err)
	require.Len(t, turn.calls, 1)
	assert.Equal(t, "call_z", turn.calls[0].ID)
	assert.Equal(t, "tool_calls", turn.finish)
	require.Len(t, turn.items, 1)
	assert.Equal(t, `{"id":"fc_9","type":"function_call","call_id":"call_z","name":"read_file","arguments":"{}"}`, string(turn.items[0]))
	require.Len(t, evs, 2)
	assert.Equal(t, EventToolUseStart, evs[0].Type)
	assert.Equal(t, EventToolUseDone, evs[1].Type)
}

const notFoundBody = `{"error":{"message":"Not found","type":"invalid_request_error"}}`

func paths(srv *fakeprovider.Server) []string {
	var out []string
	for _, r := range srv.Requests() {
		out = append(out, r.Path)
	}
	return out
}

// Review Focus 1: an endpoint without /v1/responses answers through Chat
// Completions in the same call, and is not asked again.
func TestResponsesFallsBackOn404(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Status: 404, Body: notFoundBody},
		fakeprovider.Turn{Text: "from chat"},
		fakeprovider.Turn{Text: "chat again"},
	)
	c, b := newResponsesTestClient(t, srv, "gpt-test")
	c.SetSystemPrompt("be brief")

	res, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "from chat", res.Content)
	assert.Nil(t, res.ProviderBlocks, "Chat Completions keeps no blocks")
	assert.True(t, responsesFellBack(b.baseURL))
	chatBody := srv.Requests()[1].Body
	assert.Equal(t, "system", chatBody["messages"].([]any)[0].(map[string]any)["role"], "the fallback keeps the system prompt")

	var text string
	err = c.SendMessageStreamEvents(context.Background(), userMsgs("hi"), nil, func(ev StreamEvent) { text += ev.ContentDelta })
	require.NoError(t, err)
	assert.Equal(t, "chat again", text)
	assert.Equal(t, []string{"/v1/responses", "/v1/chat/completions", "/v1/chat/completions"}, paths(srv))
}

func TestResponsesFallsBackOn400UnsupportedEndpoint(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"Unsupported endpoint: /v1/responses","type":"invalid_request_error"}}`},
		fakeprovider.Turn{Text: "from chat"},
	)
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	var text string
	err := c.SendMessageStream(context.Background(), userMsgs("hi"), nil, func(ch StreamChunk) { text += ch.Content })
	require.NoError(t, err)
	assert.Equal(t, "from chat", text)
	assert.Equal(t, []string{"/v1/responses", "/v1/chat/completions"}, paths(srv))
}

// A second backend for the same endpoint (an UpdateConfig, a subagent's
// client) goes straight to Chat Completions.
func TestResponsesFallbackIsRememberedPerEndpoint(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Status: 404, Body: notFoundBody},
		fakeprovider.Turn{Text: "one"},
		fakeprovider.Turn{Text: "two"},
	)
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)

	other := NewResponsesBackend(&Config{APIKey: "k", BaseURL: srv.BaseURL() + "/", Model: "gpt-other", Timeout: 10 * time.Second})
	res, err := other.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "two", res.Content)
	assert.Equal(t, []string{"/v1/responses", "/v1/chat/completions", "/v1/chat/completions"}, paths(srv))
}

// Review Focus 2: a retired model is an error, not a missing endpoint.
func TestResponsesModelNotFoundIsNotAFallback(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Status: 404,
		Body: `{"error":{"message":"The model 'gpt-gone' does not exist or you do not have access to it.","type":"invalid_request_error","code":"model_not_found"}}`})
	c, b := newResponsesTestClient(t, srv, "gpt-gone")
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
	assert.False(t, responsesFellBack(b.baseURL))
	assert.Equal(t, []string{"/v1/responses"}, paths(srv))
}

// Review Focus 4: stored items the endpoint no longer accepts are dropped
// for one resend, and the reply says so (BlocksRejected) so the loop strips
// the history.
func TestResponsesRetriesWithoutRefusedBlocks(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"The encrypted content for item rs_1 could not be verified.","type":"invalid_request_error","code":"invalid_encrypted_content"}}`},
		fakeprovider.Turn{Text: "ok"},
	)
	c, b := newResponsesTestClient(t, srv, "gpt-test")
	pb, err := tui.NewProviderBlocks(b.providerKey(), []json.RawMessage{
		json.RawMessage(`{"encrypted_content":"stale","id":"rs_1","summary":[],"type":"reasoning"}`),
		json.RawMessage(`{"content":[{"annotations":[],"text":"earlier","type":"output_text"}],"id":"msg_1","role":"assistant","status":"completed","type":"message"}`),
	})
	require.NoError(t, err)
	history := []tui.ChatMessage{
		{Role: "user", Content: "hi"},
		tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "earlier"}, pb),
		{Role: "user", Content: "again"},
	}
	res, err := c.SendMessageSync(context.Background(), history, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Content)
	assert.True(t, res.BlocksRejected)
	require.Len(t, srv.Requests(), 2)
	assert.Contains(t, string(srv.Requests()[0].Raw), `"encrypted_content":"stale"`)
	assert.NotContains(t, string(srv.Requests()[1].Raw), `"encrypted_content":"stale"`, "the resend is the neutral history")
	assert.Contains(t, string(srv.Requests()[1].Raw), `{"type":"message","role":"assistant","content":"earlier"}`)
	assert.False(t, responsesFellBack(b.baseURL))
}

// Without replayed blocks the same 400 is just an error: nothing to drop.
func TestResponsesRejectionWithoutReplayIsAnError(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t, fakeprovider.Turn{Status: 400,
		Body: `{"error":{"message":"The encrypted content for item rs_1 could not be verified.","type":"invalid_request_error"}}`})
	c, _ := newResponsesTestClient(t, srv, "gpt-test")
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.Error(t, err)
	assert.Len(t, srv.Requests(), 1)
}

func TestUnsupportedEndpointClassification(t *testing.T) {
	apiErr := func(status int, code, msg string) error {
		return &openai.APIError{HTTPStatusCode: status, Code: code, Message: msg}
	}
	cases := []struct {
		err  error
		want bool
	}{
		{apiErr(404, "", "Not found"), true},
		{apiErr(405, "", "Method not allowed"), true},
		{apiErr(501, "", "Not implemented"), true},
		{apiErr(400, "", "Unsupported endpoint"), true},
		{apiErr(400, "", "Invalid URL (POST /v1/responses)"), true},
		{&openai.RequestError{HTTPStatusCode: 404, Body: []byte("404 page not found")}, true},
		{apiErr(404, "model_not_found", "The model 'x' does not exist"), false},
		{apiErr(404, "", "The model 'x' does not exist"), false},
		{apiErr(404, "", "Item with id 'rs_1' not found."), false},
		{apiErr(400, "", "Invalid value for 'reasoning.effort'"), false},
		{apiErr(401, "", "Incorrect API key"), false},
		{errors.New("dial tcp: connection refused"), false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isUnsupportedEndpoint(c.err), "%v", c.err)
	}
}
