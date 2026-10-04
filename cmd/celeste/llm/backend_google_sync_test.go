package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
