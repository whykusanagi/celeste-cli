package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
)

// An agent run hands its own client config to SmallModelSummarizer. The
// summary request carries the summarizer's own system prompt on the Google
// backend, and the run's config is left as it was.
func TestSmallModelSummarizerSendsItsSystemPrompt(t *testing.T) {
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"summary"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	run := &llm.Config{
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		Backend:     llm.BackendTypeGoogle,
		Model:       "run-model",
		Collections: &config.CollectionsConfig{Enabled: true},
		XAIFeatures: &config.XAIFeaturesConfig{},
	}
	summarize := SmallModelSummarizer(run, "small-model")
	got, err := summarize(context.Background(), "SUMMARY-INSTRUCTIONS", "the transcript")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if got != "summary" {
		t.Fatalf("summary = %q, want %q", got, "summary")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(body, "SUMMARY-INSTRUCTIONS") {
		t.Fatalf("summary request carried no system prompt; body: %s", body)
	}
	// The run's config must not be mutated by the summarizer.
	if run.Collections == nil || run.Model != "run-model" {
		t.Fatalf("SmallModelSummarizer changed the run's config: %+v", run)
	}
}
