package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// anthropicOKStream is a minimal Messages stream whose reply is "ok".
const anthropicOKStream = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

// The SDK appends v1/messages itself, so a configured base URL ending in
// /v1 (what celeste advertised before 2.0) must not become /v1/v1/messages.
// Both the bare host and the /v1 form reach /v1/messages.
func TestAnthropicChatPathForBothBaseURLForms(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(anthropicOKStream))
			}))
			defer srv.Close()

			b, err := NewAnthropicBackend(&Config{APIKey: "k", BaseURL: srv.URL + suffix, Model: "claude-x"})
			require.NoError(t, err)
			res, err := b.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil)
			require.NoError(t, err)
			assert.Equal(t, "ok", res.Content)
			assert.Equal(t, "/v1/messages", gotPath)
		})
	}
}

// Both forms name one endpoint, so blocks recorded under one replay under
// the other.
func TestAnthropicProviderKeySameForBothBaseURLForms(t *testing.T) {
	bare := &AnthropicBackend{config: &Config{BaseURL: "https://api.anthropic.com", Model: "m"}}
	v1 := &AnthropicBackend{config: &Config{BaseURL: "https://api.anthropic.com/v1", Model: "m"}}
	unset := &AnthropicBackend{config: &Config{Model: "m"}}
	assert.Equal(t, bare.providerKey(), v1.providerKey())
	assert.Equal(t, bare.providerKey(), unset.providerKey())
	assert.Equal(t, bare.endpointKey(), v1.endpointKey())
}

func TestAnthropicSDKBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                                "",
		"https://api.anthropic.com":       "https://api.anthropic.com",
		"https://api.anthropic.com/":      "https://api.anthropic.com",
		"https://api.anthropic.com/v1":    "https://api.anthropic.com",
		"https://api.anthropic.com/v1/":   "https://api.anthropic.com",
		"https://proxy.example/anthropic": "https://proxy.example/anthropic",
		"https://proxy.example/x/v1":      "https://proxy.example/x",
		"https://proxy.example/v10":       "https://proxy.example/v10",
	} {
		assert.Equal(t, want, anthropicSDKBaseURL(in), in)
	}
}
