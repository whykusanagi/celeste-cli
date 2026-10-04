package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// summaryStall is the compressed stall timeout of these tests: the
// summarizer must outlive many of them on a reply that keeps streaming, as a
// cold local model's summary outlives the old fixed 3-minute deadline.
const summaryStall = 150 * time.Millisecond

func sseSummaryChunk(content string) string {
	return "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + content + "\"},\"finish_reason\":null}]}\n\n"
}

// slowModelServer behaves like a slow local model: asked to stream, it
// sends a word every gap, n words in all; asked for a whole reply, it sends
// nothing until the reply is done (n*gap later).
func slowModelServer(t *testing.T, n int, gap time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wait := func() bool {
			select {
			case <-r.Context().Done():
				return false
			case <-time.After(gap):
				return true
			}
		}
		if !strings.Contains(string(body), `"stream":true`) {
			var words []string
			for i := 0; i < n; i++ {
				if !wait() {
					return
				}
				words = append(words, fmt.Sprintf("w%d", i))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, strings.Join(words, " "))
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
			sep := " "
			if i == n-1 {
				sep = ""
			}
			fmt.Fprint(w, sseSummaryChunk(fmt.Sprintf("w%d%s", i, sep)))
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func slowSummaryConfig(url string) *llm.Config {
	return &llm.Config{APIKey: "k", BaseURL: url + "/v1", Model: "m", Backend: llm.BackendTypeOpenAI, Timeout: summaryStall}
}

// #345: a summary that keeps streaming runs as long as it streams — here
// over five stall timeouts — like a chat turn, instead of failing when a
// whole-reply request receives nothing for the stall timeout.
func TestSmallModelSummarizerOutlivesTheStallTimeoutWhileStreaming(t *testing.T) {
	srv := slowModelServer(t, 16, 50*time.Millisecond) // ~800ms, 5x the stall
	summarize := SmallModelSummarizer(slowSummaryConfig(srv.URL), "small")
	got, err := summarize(context.Background(), "system", "the transcript")
	if err != nil {
		t.Fatalf("a summary that kept streaming failed: %v", err)
	}
	if !strings.HasPrefix(got, "w0 w1") || !strings.HasSuffix(got, "w15") {
		t.Fatalf("summary = %q, want every streamed word", got)
	}
}

// A summary stream that goes silent still fails after one stall timeout.
func TestSmallModelSummarizerStallsOnASilentStream(t *testing.T) {
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
	summarize := SmallModelSummarizer(slowSummaryConfig(srv.URL), "small")
	start := time.Now()
	_, err := summarize(context.Background(), "system", "the transcript")
	if !errors.Is(err, llm.ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("a silent summary took %v to fail", took)
	}
}

// The runner bounds a summary by the summary client's request cap, not a
// fixed 3 minutes: a 600 s stall timeout (a local server's default) gives
// the summary the 30-minute cap a chat turn gets.
func TestRunnerSummaryDeadlineIsTheClientCap(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 64_000)
	runner.summaryTimeout = (&llm.Config{Timeout: 600 * time.Second}).RequestCap()
	var left time.Duration
	runner.summarize = func(ctx context.Context, _, _ string) (string, error) {
		dl, ok := ctx.Deadline()
		if !ok {
			t.Error("summary context has no deadline")
		}
		left = time.Until(dl)
		return "## Goal\nread every file", nil
	}
	msgs := []tui.ChatMessage{{Role: "user", Content: "read every file\n" + strings.Repeat("y", 40_000)}}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("toolu_%03d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%03d.go"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 24_000)})
	}
	msgs = append(msgs, tui.ChatMessage{Role: "assistant", Content: "reading on"})
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(msgs[:len(msgs)-1])
	if _, _, changed := c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(msgs) + 40_000}, false); !changed {
		t.Fatal("nothing compacted")
	}
	if left < 29*time.Minute || left > 30*time.Minute {
		t.Fatalf("summary deadline %v away, want the 30-minute cap", left)
	}
}

// NewRunner takes the summary bound from the run's client config.
func TestNewRunnerSummaryTimeoutFollowsTheProfile(t *testing.T) {
	isolateHome(t)
	cfg := fakeCfg(fakeprovider.NewOpenAI(t))
	cfg.Timeout = 1200 // 20 minutes: the cap is three times that
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	r, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.summaryTimeout != 60*time.Minute {
		t.Fatalf("summaryTimeout = %v, want 60m", r.summaryTimeout)
	}
}
