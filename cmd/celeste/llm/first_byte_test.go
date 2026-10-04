package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// delayedServer waits delay before sending anything, then streams a short
// reply: a local model prefilling a long prompt. With headersFirst the
// status line and headers go out at once and only the body waits (servers
// that flush SSE headers before the model has produced a token).
func delayedServer(t *testing.T, delay time.Duration, headersFirst bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Read the request so the server notices the client hanging up.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		if headersFirst {
			w.WriteHeader(http.StatusOK)
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		for i := 0; i < 3; i++ {
			fmt.Fprint(w, sseChunk(fmt.Sprintf(`{"content":"w%d "}`, i)))
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func firstByteClient(url string, stall, firstByte time.Duration) *Client {
	return NewClient(&Config{APIKey: "k", BaseURL: url + "/v1", Model: "m", Backend: BackendTypeOpenAI, Timeout: stall, FirstByteTimeout: firstByte}, nil)
}

// #359: a cold prefill on a loaded machine sends nothing for longer than the
// between-chunk stall timeout. The first byte gets its own, larger budget.
func TestFirstByteBudgetOutlastsStallTimeout(t *testing.T) {
	for _, headersFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("headersFirst=%v", headersFirst), func(t *testing.T) {
			var calls atomic.Int32
			srv := delayedServer(t, 400*time.Millisecond, headersFirst, &calls)
			got, err := streamText(t, firstByteClient(srv.URL, 100*time.Millisecond, 5*time.Second))
			if err != nil {
				t.Fatalf("first byte after 400ms failed under a 5s first-byte budget: %v", err)
			}
			if !strings.Contains(got, "w2") {
				t.Fatalf("reply = %q", got)
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("requests = %d, want 1", n)
			}
		})
	}
}

// Once data flows, the stall timeout applies again: a stream that goes
// silent mid-reply fails after the stall timeout, not the first-byte budget.
func TestStallAfterFirstByteUsesStallTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := stallServer(t, &calls)
	start := time.Now()
	_, err := streamText(t, firstByteClient(srv.URL, 150*time.Millisecond, 8*time.Second))
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("mid-stream stall took %v to detect; the first-byte budget must not apply after the first byte", d)
	}
	if !strings.Contains(err.Error(), "150ms") {
		t.Fatalf("error should name the stall timeout: %v", err)
	}
}

// A server that never sends a byte fails after the first-byte budget, with
// a message that says it was waiting for the first byte.
func TestNoFirstByteFailsAfterFirstByteBudget(t *testing.T) {
	var calls atomic.Int32
	srv := delayedServer(t, 10*time.Second, false, &calls)
	start := time.Now()
	_, err := streamText(t, firstByteClient(srv.URL, 50*time.Millisecond, 300*time.Millisecond))
	d := time.Since(start)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if d < 300*time.Millisecond {
		t.Fatalf("failed after %v, before the 300ms first-byte budget", d)
	}
	if d > 3*time.Second {
		t.Fatalf("took %v, want about the 300ms first-byte budget", d)
	}
	if !strings.Contains(err.Error(), "first byte") || !strings.Contains(err.Error(), "300ms") {
		t.Fatalf("error should say it waited 300ms for the first byte: %v", err)
	}
	if !strings.Contains(err.Error(), "also lengthens the stall timeout") {
		t.Fatalf("the hint to raise the timeout should say it also lengthens the stall timeout: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1: a first-byte stall is not retried", n)
	}
}

// When the first-byte budget reaches the request cap, a request that never
// sent a byte is still reported as a first-byte stall, not as a reply that
// ran too long.
func TestFirstByteBudgetAtTheCapReportsTheFirstByte(t *testing.T) {
	err := withRetry(context.Background(), retryOpts{stall: 10 * time.Millisecond, firstByte: 80 * time.Millisecond, timeout: 80 * time.Millisecond}, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, func(time.Duration) {})
	if !errors.Is(err, ErrStalled) || !strings.Contains(err.Error(), "first byte") {
		t.Fatalf("err = %v, want a first-byte stall", err)
	}
}

// A budget no longer than the stall timeout changes nothing (hosted
// providers): the stall timeout covers the first byte as before.
func TestConfigFirstByteBudget(t *testing.T) {
	var none *Config
	for _, tc := range []struct {
		cfg  *Config
		want time.Duration
	}{
		{none, 60 * time.Second},
		{&Config{}, 60 * time.Second},
		{&Config{Timeout: 90 * time.Second}, 90 * time.Second},
		{&Config{Timeout: 90 * time.Second, FirstByteTimeout: 30 * time.Second}, 90 * time.Second},
		{&Config{Timeout: 600 * time.Second, FirstByteTimeout: 30 * time.Minute}, 30 * time.Minute},
		// capped by the request cap (MaxRequestDuration of the stall timeout)
		{&Config{Timeout: 600 * time.Second, FirstByteTimeout: 2 * time.Hour}, 30 * time.Minute},
		{&Config{Timeout: 20 * time.Minute, FirstByteTimeout: 2 * time.Hour}, 60 * time.Minute},
	} {
		if got := tc.cfg.FirstByteBudget(); got != tc.want {
			t.Errorf("FirstByteBudget(%+v) = %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

// The first-byte budget runs from the request, not from the headers: a
// server that sends its headers late and its body later still fails once
// the budget is spent.
func TestFirstByteBudgetIsNotExtendedByHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		fmt.Fprint(w, sseChunk(`{"content":"late"}`))
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(srv.Close)
	_, err := streamText(t, firstByteClient(srv.URL, 50*time.Millisecond, 350*time.Millisecond))
	if !errors.Is(err, ErrStalled) || !strings.Contains(err.Error(), "first byte") {
		t.Fatalf("err = %v, want a first-byte stall: 500ms to the first byte exceeds the 350ms budget", err)
	}
}
