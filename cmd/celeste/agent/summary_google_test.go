package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
)

// slowGeminiServer behaves like a Gemini endpoint generating a long reply
// slowly: streamGenerateContent sends a word every gap, n words in all;
// generateContent sends nothing until the whole reply is done (n*gap).
func slowGeminiServer(t *testing.T, n int, gap time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := func() bool {
			select {
			case <-r.Context().Done():
				return false
			case <-time.After(gap):
				return true
			}
		}
		word := func(i int) string {
			if i == n-1 {
				return fmt.Sprintf("w%d", i)
			}
			return fmt.Sprintf("w%d ", i)
		}
		if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			var sb strings.Builder
			for i := 0; i < n; i++ {
				if !wait() {
					return
				}
				sb.WriteString(word(i))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]},"finishReason":"STOP"}]}`, sb.String())
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		for i := 0; i < n; i++ {
			if !wait() {
				return
			}
			finish := ""
			if i == n-1 {
				finish = `,"finishReason":"STOP"`
			}
			fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":%q}]}%s}]}\n\n", word(i), finish)
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func geminiSummaryConfig(url string) *llm.Config {
	return &llm.Config{APIKey: "k", BaseURL: url, Model: "gemini-test", Backend: llm.BackendTypeGoogle, Timeout: summaryStall}
}

// #349: a Gemini summary that keeps generating outlives the stall timeout,
// as the OpenAI-compatible one does (#345), instead of failing with
// ErrStalled because the whole-reply request receives nothing meanwhile.
func TestSmallModelSummarizerOutlivesTheStallTimeoutOnGoogle(t *testing.T) {
	srv := slowGeminiServer(t, 16, 50*time.Millisecond) // ~800ms, 5x the stall
	summarize := SmallModelSummarizer(geminiSummaryConfig(srv.URL), "gemini-test")
	got, err := summarize(context.Background(), "system", "the transcript")
	if err != nil {
		t.Fatalf("a Gemini summary that kept streaming failed: %v", err)
	}
	if !strings.HasPrefix(got, "w0 w1") || !strings.HasSuffix(got, "w15") {
		t.Fatalf("summary = %q, want every streamed word", got)
	}
}

// A Gemini summary stream that goes silent still fails after one stall
// timeout (#348's bound is kept).
func TestSmallModelSummarizerStallsOnASilentGoogleStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	summarize := SmallModelSummarizer(geminiSummaryConfig(srv.URL), "gemini-test")
	start := time.Now()
	_, err := summarize(context.Background(), "system", "the transcript")
	if !errors.Is(err, llm.ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("a silent summary took %v to fail", took)
	}
}
