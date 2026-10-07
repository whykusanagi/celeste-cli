package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// #312: the 1-hour part of an Anthropic cache write is kept, so it is
// priced at the 1-hour rate.
func TestUsageTrackerKeepsOneHourCacheWrites(t *testing.T) {
	var u usageTracker
	u.start(anthropic.Usage{InputTokens: 12, CacheCreationInputTokens: 300,
		CacheCreation: anthropic.CacheCreation{Ephemeral1hInputTokens: 200, Ephemeral5mInputTokens: 100}})
	u.delta(anthropic.MessageDeltaUsage{OutputTokens: 5})
	got := u.result()
	require.NotNil(t, got)
	assert.Equal(t, 300, got.CacheWriteTokens)
	assert.Equal(t, 200, got.CacheWrite1hTokens)
}

func doneUsage(evs []StreamEvent) *TokenUsage {
	for _, ev := range evs {
		if ev.Type == EventMessageDone {
			return ev.Usage
		}
	}
	return nil
}

// #312: Chat Completions servers report cached prompt tokens in
// prompt_tokens_details.cached_tokens (OpenAI, xAI); every path keeps them.
func TestOpenAIReportsCachedPromptTokens(t *testing.T) {
	b, _ := newThinkBackend(t, fakeprovider.Turn{Text: "hi"}, fakeprovider.Turn{Text: "hi"}, fakeprovider.Turn{Text: "hi"})
	u := doneUsage(streamEvents(t, b, hello))
	require.NotNil(t, u)
	assert.Equal(t, 100, u.PromptTokens)
	assert.Equal(t, 40, u.CacheReadTokens)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var final *TokenUsage
	require.NoError(t, b.SendMessageStream(ctx, hello, nil, func(c StreamChunk) {
		if c.IsFinal {
			final = c.Usage
		}
	}))
	require.NotNil(t, final)
	assert.Equal(t, 40, final.CacheReadTokens)

	res, err := b.SendMessageSync(ctx, hello, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Usage)
	assert.Equal(t, 40, res.Usage.CacheReadTokens)
}

func TestXAIReportsCachedPromptTokens(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi"}, fakeprovider.Turn{Text: "hi"})
	b, err := NewXAIBackend(&Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "grok-4-1-fast", Timeout: 30}, tools.NewRegistry())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var evs []StreamEvent
	require.NoError(t, b.SendMessageStreamEvents(ctx, hello, nil, func(ev StreamEvent) { evs = append(evs, ev) }))
	u := doneUsage(evs)
	require.NotNil(t, u)
	assert.Equal(t, 40, u.CacheReadTokens)

	var final *TokenUsage
	require.NoError(t, b.SendMessageStream(ctx, hello, nil, func(c StreamChunk) {
		if c.IsFinal {
			final = c.Usage
		}
	}))
	require.NotNil(t, final)
	assert.Equal(t, 40, final.CacheReadTokens)
}

// #312: Gemini reports usage on its stream chunks, cached content
// included (cachedContentTokenCount, part of promptTokenCount); the
// backend used to report none at all.
func TestGoogleReportsUsageWithCachedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":1000}}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" world\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1000,\"cachedContentTokenCount\":600,\"candidatesTokenCount\":20,\"thoughtsTokenCount\":5,\"totalTokenCount\":1025}}\n\n")
	}))
	t.Cleanup(srv.Close)
	client := NewClient(&Config{APIKey: "k", BaseURL: srv.URL, Model: "gemini-test", Backend: BackendTypeGoogle}, nil)

	var final *TokenUsage
	require.NoError(t, client.SendMessageStream(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil, func(c StreamChunk) {
		if c.IsFinal {
			final = c.Usage
		}
	}))
	require.NotNil(t, final)
	assert.Equal(t, 1000, final.PromptTokens)
	assert.Equal(t, 25, final.CompletionTokens, "thoughts are billed as output")
	assert.Equal(t, 1025, final.TotalTokens)
	assert.Equal(t, 600, final.CacheReadTokens)
}
