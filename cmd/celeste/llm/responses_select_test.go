package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestNewClientPicksResponsesForOpenAI(t *testing.T) {
	cases := []struct {
		base      string
		responses bool
	}{
		{"https://api.openai.com/v1", true},
		{"", true}, // go-openai's default endpoint is OpenAI's
		{"https://api.venice.ai/api/v1", false},
		{"https://openrouter.ai/api/v1", false},
		{"http://127.0.0.1:8080/v1", false},
	}
	for _, tc := range cases {
		c := NewClient(&Config{APIKey: "k", BaseURL: tc.base, Model: "m"}, nil)
		_, isResponses := c.backend.(*ResponsesBackend)
		assert.Equal(t, tc.responses, isResponses, "%q: backend %T", tc.base, c.backend)
		want := BackendTypeOpenAI
		if tc.responses {
			want = BackendTypeOpenAIResponses
		}
		assert.Equal(t, want, c.backendType, tc.base)
	}
}

func TestBackendOverrideSelectsResponses(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1", Model: "m", Backend: BackendTypeOpenAIResponses}, nil)
	assert.IsType(t, &ResponsesBackend{}, c.backend)
	assert.Equal(t, BackendTypeOpenAIResponses, c.backendType)
}

// Review Focus 5 (and ruling 12): /set-model sends the new model from the
// next request on, with the same prompt and thinking config, and the old
// model's items are not replayed to the new one.
func TestUpdateConfigRebuildsBackendForModel(t *testing.T) {
	resetResponsesFallback()
	t.Cleanup(resetResponsesFallback)
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Reasoning: &fakeprovider.Reasoning{ID: "rs_1", Summary: "s", Encrypted: "enc-1"}, Text: "a"},
		fakeprovider.Turn{Text: "b"},
	)
	cfg := func(model string) *Config {
		return &Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: model, Timeout: 10 * time.Second, Backend: BackendTypeOpenAIResponses}
	}
	c := NewClient(cfg("gpt-5"), nil)
	c.SetSystemPrompt("sys")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "high"})
	res, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	history := []tui.ChatMessage{
		{Role: "user", Content: "hi"},
		tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "a"}, res.ProviderBlocks),
		{Role: "user", Content: "more"},
	}

	c.UpdateConfig(cfg("gpt-5-mini"))
	_, err = c.SendMessageSync(context.Background(), history, nil)
	require.NoError(t, err)

	reqs := srv.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "gpt-5-mini", reqs[1].Body["model"])
	assert.Equal(t, "sys", reqs[1].Body["instructions"])
	assert.Equal(t, map[string]any{"effort": "high"}, reqs[1].Body["reasoning"])
	assert.NotContains(t, string(reqs[1].Raw), "enc-1", "gpt-5's reasoning item was sent to gpt-5-mini")
}

func TestUpdateConfigSwitchesTransport(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "https://api.venice.ai/api/v1", Model: "m"}, nil)
	assert.IsType(t, &OpenAIBackend{}, c.backend)
	c.UpdateConfig(&Config{APIKey: "k", BaseURL: "https://api.openai.com/v1", Model: "gpt-5"})
	assert.IsType(t, &ResponsesBackend{}, c.backend)
	c.UpdateConfig(&Config{APIKey: "k", BaseURL: "https://api.venice.ai/api/v1", Model: "m"})
	assert.IsType(t, &OpenAIBackend{}, c.backend)
}

func TestUpdateConfigKeepsBackendWhenNothingChanged(t *testing.T) {
	c := NewClient(&Config{APIKey: "k", BaseURL: "https://api.openai.com/v1", Model: "gpt-5"}, nil)
	before := c.backend
	c.UpdateConfig(&Config{APIKey: "k", BaseURL: "https://api.openai.com/v1", Model: "gpt-5", Timeout: time.Minute})
	assert.Same(t, before, c.backend)
}
