package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sseTestToken = "test-token-0123456789abcdef0123456789abcdef0123456789abcdef01234567"

func newSSETestServer(t *testing.T, rate int) (*httptest.Server, *sseHandler) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.RateLimit = rate
	s := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	h := s.newSSEHandler(ctx, sseTestToken)
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		cancel()
		ts.CloseClientConnections()
		ts.Close()
	})
	return ts, h
}

// openSSEStream opens GET /sse and returns the response and the endpoint
// the server announced.
func openSSEStream(t *testing.T, ts *httptest.Server) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/sse", nil)
	req.Header.Set("Authorization", "Bearer "+sseTestToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return resp, ""
	}
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read endpoint event: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			return resp, strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
	}
}

func postMessage(t *testing.T, ts *httptest.Server, endpoint, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+endpoint, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sseTestToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", endpoint, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestSSEPostDoesNotBlockOnGoneStream: a POST whose stream has closed with
// a full event buffer returns instead of blocking forever (Aikido 806869667).
func TestSSEPostDoesNotBlockOnGoneStream(t *testing.T) {
	_, h := newSSETestServer(t, 60)
	conn := &sseConnection{
		id:     "conn-stalled",
		events: make(chan []byte, 1),
		bucket: newTokenBucket(60),
		done:   make(chan struct{}),
	}
	conn.events <- []byte("full")
	close(conn.done) // the stream's handler has returned
	h.connections.Store(conn.id, conn)

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/message?connectionId="+conn.id, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"nope"}`))
		req.Header.Set("Authorization", "Bearer "+sseTestToken)
		h.ServeHTTP(rec, req)
		done <- rec.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusAccepted {
			t.Fatalf("POST status = %d, want 202", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("POST blocked on a closed stream with a full buffer")
	}
}

// TestSSEStreamCap: the server holds at most maxSSEConnections streams; one
// more is refused (Aikido 806869493).
func TestSSEStreamCap(t *testing.T) {
	ts, _ := newSSETestServer(t, 600)
	for i := 0; i < maxSSEConnections; i++ {
		resp, ep := openSSEStream(t, ts)
		if resp.StatusCode != http.StatusOK || ep == "" {
			t.Fatalf("stream %d: status %d", i, resp.StatusCode)
		}
	}
	resp, _ := openSSEStream(t, ts)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stream %d: status %d, want 503", maxSSEConnections+1, resp.StatusCode)
	}
}

// TestSSERateLimitIsServerWide: opening a second stream does not double the
// configured request rate (Aikido 806869493).
func TestSSERateLimitIsServerWide(t *testing.T) {
	ts, _ := newSSETestServer(t, 2)
	_, ep1 := openSSEStream(t, ts)
	_, ep2 := openSSEStream(t, ts)
	body := `{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`
	codes := []int{
		postMessage(t, ts, ep1, body),
		postMessage(t, ts, ep2, body),
		postMessage(t, ts, ep2, body),
	}
	if codes[0] != http.StatusAccepted || codes[1] != http.StatusAccepted {
		t.Fatalf("first two POSTs = %v, want 202", codes[:2])
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third POST across two streams = %d, want 429 (rate is server-wide)", codes[2])
	}
}

// TestSSEPostRejectsOversizedBody: a POST body over 1 MiB is refused, not
// cut to its first MiB and run.
func TestSSEPostRejectsOversizedBody(t *testing.T) {
	ts, _ := newSSETestServer(t, 60)
	_, ep := openSSEStream(t, ts)
	body := `{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}` + strings.Repeat(" ", 1<<20)
	if code := postMessage(t, ts, ep, body); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST of an oversized body = %d, want 413", code)
	}
}
