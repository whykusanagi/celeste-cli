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

// An agent run hands its own client config to SmallModelSummarizer. With
// skip_persona_prompt set, the Google backend sends no system instruction,
// so a summary built on that config lost its instructions. The summarizer
// must send its system prompt whatever the run's persona setting is.
func TestSmallModelSummarizerSendsSystemPromptDespitePersonaSkip(t *testing.T) {
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
		APIKey:            "test-key",
		BaseURL:           srv.URL,
		Backend:           llm.BackendTypeGoogle,
		Model:             "run-model",
		SkipPersonaPrompt: true,
		Collections:       &config.CollectionsConfig{Enabled: true},
		XAIFeatures:       &config.XAIFeaturesConfig{},
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
	if !run.SkipPersonaPrompt || run.Collections == nil || run.Model != "run-model" {
		t.Fatalf("SmallModelSummarizer changed the run's config: %+v", run)
	}
}
