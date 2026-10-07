package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// sseChunk is one OpenAI chat-completions stream event.
func sseChunk(delta string) string {
	return "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":" + delta + ",\"finish_reason\":null}]}\n\n"
}

// slowServer streams n events, one every gap, then ends the stream. reasoning
// sends the events as `reasoning` deltas (what Ollama sends while a model
// thinks), which celeste never hands to a callback, before one content
// delta.
func slowServer(t *testing.T, n int, gap time.Duration, reasoning bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		for i := 0; i < n; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(gap):
			}
			if reasoning {
				fmt.Fprint(w, sseChunk(`{"reasoning":"hmm "}`))
			} else {
				fmt.Fprint(w, sseChunk(fmt.Sprintf(`{"content":"w%d "}`, i)))
			}
			fl.Flush()
		}
		if reasoning {
			fmt.Fprint(w, sseChunk(`{"content":"done"}`))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stallServer sends one content event and then nothing until the client
// goes away: a dead upstream, or a model that hangs mid-reply.
func stallServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(`{"content":"start "}`))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func openAIClient(url string, timeout time.Duration) *Client {
	return NewClient(&Config{APIKey: "k", BaseURL: url + "/v1", Model: "m", Backend: BackendTypeOpenAI, Timeout: timeout}, nil)
}

func streamText(t *testing.T, c *Client) (string, error) {
	t.Helper()
	var b strings.Builder
	err := c.SendMessageStream(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi"}}, nil, func(ch StreamChunk) {
		b.WriteString(ch.Content)
	})
	return b.String(), err
}

// L1: a stream that keeps sending runs past the configured timeout. The
// timeout is how long the model may go silent, not how long a reply may
// take: a slow local model streaming at a few tokens a second must finish.
func TestStreamOutlivesTimeoutWhileBytesFlow(t *testing.T) {
	var calls atomic.Int32
	srv := slowServer(t, 10, 40*time.Millisecond, false, &calls)
	got, err := streamText(t, openAIClient(srv.URL, 150*time.Millisecond))
	if err != nil {
		t.Fatalf("stream failed although bytes kept arriving: %v", err)
	}
	if !strings.Contains(got, "w9") {
		t.Fatalf("reply cut short: %q", got)
	}
}

// Reasoning deltas never reach a callback, but they are bytes on the wire:
// a model thinking out loud is alive, so the stall timer must not fire.
func TestReasoningBytesKeepTheStreamAlive(t *testing.T) {
	var calls atomic.Int32
	srv := slowServer(t, 10, 40*time.Millisecond, true, &calls)
	got, err := streamText(t, openAIClient(srv.URL, 150*time.Millisecond))
	if err != nil {
		t.Fatalf("stream failed while the model was still sending reasoning: %v", err)
	}
	if got != "done" {
		t.Fatalf("reply = %q, want done", got)
	}
}

// A stream that goes silent fails after the timeout, once, with a message
// that names the stall and the knob. A dead connection still fails fast.
func TestStalledStreamFailsAfterTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := stallServer(t, &calls)
	start := time.Now()
	_, err := streamText(t, openAIClient(srv.URL, 150*time.Millisecond))
	if err == nil {
		t.Fatal("a stalled stream must fail")
	}
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if !strings.Contains(err.Error(), "150ms") || !strings.Contains(err.Error(), "--set-timeout") {
		t.Fatalf("error should name the stall timeout and how to raise it: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("stall took %v to detect, want about the 150ms timeout", d)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1: a stall is not retried", n)
	}
}

// A server that accepts the request but never answers (cold prefill on a
// local model, or a dead upstream) counts as silent from the start.
func TestNoResponseAtAllFailsAfterTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	_, err := streamText(t, openAIClient(srv.URL, 150*time.Millisecond))
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v, want about the 150ms timeout", d)
	}
}

// The stall timer resets on activity; it fires only after a full idle
// period.
func TestWithRetryStallResetsOnActivity(t *testing.T) {
	attempts := 0
	// Windows sleeps in ~15.6 ms ticks, so the gap between touches is kept
	// far below the stall window: 30 touches over ≥150 ms, each gap ≤ ~32 ms.
	err := withRetry(context.Background(), retryOpts{stall: 150 * time.Millisecond, timeout: time.Minute}, func(ctx context.Context) error {
		attempts++
		for i := 0; i < 30; i++ {
			time.Sleep(5 * time.Millisecond)
			touchStall(ctx)
		}
		return ctx.Err()
	}, func(time.Duration) {})
	if err != nil || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want nil,1", err, attempts)
	}
}

// A stall ends the attempt with ErrStalled and is not retried.
func TestWithRetryStallIsNotRetried(t *testing.T) {
	attempts := 0
	err := withRetry(context.Background(), retryOpts{stall: 20 * time.Millisecond, timeout: time.Minute}, func(ctx context.Context) error {
		attempts++
		<-ctx.Done()
		return ctx.Err()
	}, func(time.Duration) {})
	if !errors.Is(err, ErrStalled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want ErrStalled,1", err, attempts)
	}
}

// A provider that already reports the stall (its error wraps ErrStalled)
// gets the stall message once, not twice.
func TestWithRetryStallMessageIsNotRepeated(t *testing.T) {
	err := withRetry(context.Background(), retryOpts{stall: 20 * time.Millisecond, timeout: time.Minute}, func(ctx context.Context) error {
		<-ctx.Done()
		return fmt.Errorf("%w after 20.4ms", context.Cause(ctx))
	}, func(time.Duration) {})
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if n := strings.Count(err.Error(), ErrStalled.Error()); n != 1 {
		t.Fatalf("stall message appears %d times: %v", n, err)
	}
}

// However steadily a reply streams, one request is still bounded.
func TestWithRetryHardCapBoundsAStreamingAttempt(t *testing.T) {
	err := withRetry(context.Background(), retryOpts{stall: 30 * time.Millisecond, timeout: 80 * time.Millisecond}, func(ctx context.Context) error {
		for ctx.Err() == nil {
			time.Sleep(5 * time.Millisecond)
			touchStall(ctx)
		}
		return ctx.Err()
	}, func(time.Duration) {})
	if err == nil || errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want the hard cap", err)
	}
	if !strings.Contains(err.Error(), "80ms") {
		t.Fatalf("error should name the cap: %v", err)
	}
}

func TestMaxRequestDuration(t *testing.T) {
	if got := MaxRequestDuration(60 * time.Second); got != 30*time.Minute {
		t.Errorf("cap for 60s = %v, want 30m", got)
	}
	if got := MaxRequestDuration(20 * time.Minute); got != 60*time.Minute {
		t.Errorf("cap for 20m = %v, want 60m (3x)", got)
	}
}

// A config's stall timeout is its Timeout, 60 s when it carries none; its
// request cap is MaxRequestDuration of that (#345: summaries use both).
func TestConfigStallTimeoutAndRequestCap(t *testing.T) {
	var none *Config
	for _, tc := range []struct {
		cfg        *Config
		stall, cap time.Duration
	}{
		{none, 60 * time.Second, 30 * time.Minute},
		{&Config{}, 60 * time.Second, 30 * time.Minute},
		{&Config{Timeout: 600 * time.Second}, 600 * time.Second, 30 * time.Minute},
		{&Config{Timeout: 20 * time.Minute}, 20 * time.Minute, 60 * time.Minute},
	} {
		if got := tc.cfg.StallTimeout(); got != tc.stall {
			t.Errorf("StallTimeout(%+v) = %v, want %v", tc.cfg, got, tc.stall)
		}
		if got := tc.cfg.RequestCap(); got != tc.cap {
			t.Errorf("RequestCap(%+v) = %v, want %v", tc.cfg, got, tc.cap)
		}
	}
}
