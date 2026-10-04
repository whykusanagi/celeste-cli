package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// #349: the Google sync path streams and assembles the whole reply — text
// from every chunk, complete function calls with their thought signatures,
// and the provider's finish reason.
func TestGoogleSendMessageSyncAssemblesTheStream(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello \"}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"world\"}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"read_file\",\"args\":{\"path\":\"a.go\"}},\"thoughtSignature\":\"c2ln\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{APIKey: "k", BaseURL: srv.URL, Model: "gemini-test", Backend: BackendTypeGoogle}, nil)
	res, err := client.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	require.NoError(t, err)
	require.Len(t, paths, 1)
	assert.True(t, strings.HasSuffix(paths[0], ":streamGenerateContent"), "sync request went to %s", paths[0])
	assert.Equal(t, "hello world", res.Content)
	assert.Equal(t, "STOP", res.FinishReason)
	require.Len(t, res.ToolCalls, 1)
	assert.Equal(t, "read_file", res.ToolCalls[0].Name)
	assert.JSONEq(t, `{"path":"a.go"}`, res.ToolCalls[0].Arguments)
	assert.Equal(t, []byte("sig"), res.ToolCalls[0].ThoughtSignature)
}

// A stream error surfaces as the request's error, not a partial result.
func TestGoogleSendMessageSyncReportsAStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":400,"message":"bad model","status":"INVALID_ARGUMENT"}}`)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{APIKey: "k", BaseURL: srv.URL, Model: "gemini-test", Backend: BackendTypeGoogle}, nil)
	res, err := client.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "bad model")
}

// thoughtServer streams one thought part and then the answer, the way
// Gemini does with ThinkingConfig.IncludeThoughts on.
func thoughtServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"pondering the request\",\"thought\":true}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"the answer\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// With thinking on, Gemini's thoughts are not reply text: a sync result (a
// summary) and the chat stream carry only the answer.
func TestGoogleThoughtsAreNotReplyText(t *testing.T) {
	client := NewClient(&Config{APIKey: "k", BaseURL: thoughtServer(t).URL, Model: "gemini-test", Backend: BackendTypeGoogle}, nil)
	msgs := []tui.ChatMessage{{Role: "user", Content: "hi"}}

	res, err := client.SendMessageSync(context.Background(), msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, "the answer", res.Content)

	var streamed strings.Builder
	require.NoError(t, client.SendMessageStream(context.Background(), msgs, nil, func(ch StreamChunk) {
		streamed.WriteString(ch.Content)
	}))
	assert.Equal(t, "the answer", streamed.String())
}

// The event stream reports Gemini's thoughts as thinking, the way the
// OpenAI-compatible backend reports reasoning, never as content.
func TestGoogleStreamEventsReportThoughtsAsThinking(t *testing.T) {
	client := NewClient(&Config{APIKey: "k", BaseURL: thoughtServer(t).URL, Model: "gemini-test", Backend: BackendTypeGoogle}, nil)
	var content, thinking strings.Builder
	err := client.SendMessageStreamEvents(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil, func(ev StreamEvent) {
		switch ev.Type {
		case EventContentDelta:
			content.WriteString(ev.ContentDelta)
		case EventThinkingDelta:
			thinking.WriteString(ev.ThinkingDelta)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "the answer", content.String())
	assert.Equal(t, "pondering the request", thinking.String())
}

// The Google SDK's HTTP client reports bytes to the stall watch like every
// other backend's: a response that is still sending (here, keep-alive blank
// lines before the first chunk) is alive even when no chunk has arrived yet.
func TestGoogleResponseBytesKeepTheStallWatchAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		for i := 0; i < 8; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(40 * time.Millisecond):
			}
			fmt.Fprint(w, "\n")
			fl.Flush()
		}
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"done\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&Config{APIKey: "k", BaseURL: srv.URL, Model: "gemini-test", Backend: BackendTypeGoogle, Timeout: 150 * time.Millisecond}, nil)
	res, err := client.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil)
	require.NoError(t, err, "the response was still sending bytes")
	assert.Equal(t, "done", res.Content)
}
