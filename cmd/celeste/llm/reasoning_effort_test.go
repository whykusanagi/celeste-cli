package llm

import (
	"context"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// An endpoint without the Responses API falls back to Chat Completions;
// gpt-5 keeps its reasoning effort there (audit #7 D5: the chat backend
// only knew the o-series).
func TestResponsesFallbackKeepsReasoningEffort(t *testing.T) {
	srv := fakeprovider.NewOpenAIResponses(t,
		fakeprovider.Turn{Status: 404, Body: notFoundBody},
		fakeprovider.Turn{Text: "from chat"},
	)
	c, _ := newResponsesTestClient(t, srv, "gpt-5")
	c.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "medium"})
	_, err := c.SendMessageSync(context.Background(), userMsgs("hi"), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"/v1/responses", "/v1/chat/completions"}, paths(srv))
	assert.Equal(t, "medium", srv.Requests()[1].Body["reasoning_effort"])
}

func TestReasoningEffortLevels(t *testing.T) {
	on := func(level string) ThinkingConfig { return ThinkingConfig{Enabled: true, Level: level} }
	for _, c := range []struct {
		tc   ThinkingConfig
		want string
	}{
		{on("low"), "low"}, {on("medium"), "medium"}, {on("high"), "high"}, {on("max"), "high"},
		{on("off"), ""}, {on(""), ""}, {ThinkingConfig{Level: "high"}, ""},
	} {
		assert.Equal(t, c.want, reasoningEffort(c.tc), "%+v", c.tc)
	}
}

// Chat Completions sends reasoning_effort to the models that take it: the
// o-series and gpt-5 (not gpt-5-chat), as the Responses backend does.
func TestChatCompletionsReasoningEffortModels(t *testing.T) {
	for model, want := range map[string]string{
		"o3": "high", "o4-mini": "high", "gpt-5": "high", "gpt-5-mini": "high",
		"gpt-5-chat-latest": "", "gpt-4o": "", "llama-3": "",
	} {
		b := NewOpenAIBackend(&Config{APIKey: "k", Model: model})
		b.SetThinkingConfig(ThinkingConfig{Enabled: true, Level: "max"})
		req := openai.ChatCompletionRequest{Model: model}
		b.applyThinkingConfig(&req)
		assert.Equal(t, want, req.ReasoningEffort, model)
	}
}
