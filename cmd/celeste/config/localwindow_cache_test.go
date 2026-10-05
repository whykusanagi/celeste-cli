package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ollamaToggle is a fake Ollama whose model is loaded (32768 in /api/ps)
// until unload is called; then /api/ps lists nothing and /api/show has no
// num_ctx, as after Ollama's idle keep_alive.
func ollamaToggle(t *testing.T) (srv *httptest.Server, unload func(), hits *atomic.Int32) {
	t.Helper()
	var unloaded atomic.Bool
	hits = new(atomic.Int32)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/api/ps":
			if unloaded.Load() {
				_, _ = w.Write([]byte(`{"models":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:14b","model":"qwen3:14b","context_length":32768}]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"parameters":"temperature 0.6"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() { unloaded.Store(true) }, hits
}

// #310 review: Ollama unloads an idle model after five minutes; the probe
// then hears nothing. The window must stay what the server last reported,
// not drop to the 8,192 fallback until the model loads again.
func TestCachedProbeKeepsTheLastReportedWindow(t *testing.T) {
	srv, unload, _ := ollamaToggle(t)
	c := newWindowCache(http.DefaultClient, 20*time.Millisecond)
	if got := c.get(srv.URL+"/v1", "", "qwen3:14b"); got != 32768 {
		t.Fatalf("loaded: got %d", got)
	}
	unload()
	for range 3 {
		time.Sleep(30 * time.Millisecond)
		if got := c.get(srv.URL+"/v1", "", "qwen3:14b"); got != 32768 {
			t.Fatalf("after the unload: got %d, want 32768", got)
		}
		c.wait(srv.URL+"/v1", "qwen3:14b")
	}
	if got := c.get(srv.URL+"/v1", "", "qwen3:14b"); got != 32768 {
		t.Fatalf("after the refreshes: got %d, want 32768", got)
	}
}

// A different positive answer replaces the old one (a server restarted with
// another window).
func TestCachedProbeTakesANewWindow(t *testing.T) {
	var n atomic.Int64
	n.Store(16384)
	c := &windowCache{ttl: time.Millisecond, entries: map[string]*windowEntry{},
		probe: func(context.Context, string, string, string) int { return int(n.Load()) }}
	if got := c.get("http://127.0.0.1:1/v1", "", "m"); got != 16384 {
		t.Fatalf("got %d", got)
	}
	n.Store(65536)
	time.Sleep(5 * time.Millisecond)
	c.get("http://127.0.0.1:1/v1", "", "m")
	c.wait("http://127.0.0.1:1/v1", "m")
	if got := c.get("http://127.0.0.1:1/v1", "", "m"); got != 65536 {
		t.Fatalf("got %d, want 65536", got)
	}
}

// Callers asking at the same moment share one probe (the TUI and a turn's
// goroutine used to probe together).
func TestCachedProbeAsksOnceForConcurrentCallers(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c := &windowCache{ttl: time.Minute, entries: map[string]*windowEntry{},
		probe: func(context.Context, string, string, string) int {
			calls.Add(1)
			<-release
			return 12288
		}}
	var wg sync.WaitGroup
	got := make([]int, 8)
	for i := range got {
		wg.Add(1)
		go func() { defer wg.Done(); got[i] = c.get("http://127.0.0.1:1/v1", "", "m") }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("%d probes, want 1", calls.Load())
	}
	for _, g := range got {
		if g != 12288 {
			t.Fatalf("got %v", got)
		}
	}
}

// An answer past its TTL is returned at once and refreshed in the
// background: no caller waits on the network for an endpoint it already
// has an answer for.
func TestCachedProbeRefreshesInTheBackground(t *testing.T) {
	var calls atomic.Int32
	block := make(chan struct{})
	c := &windowCache{ttl: time.Millisecond, entries: map[string]*windowEntry{},
		probe: func(context.Context, string, string, string) int {
			if calls.Add(1) > 1 {
				<-block
			}
			return 12288
		}}
	c.get("http://127.0.0.1:1/v1", "", "m")
	time.Sleep(5 * time.Millisecond)
	done := make(chan int, 1)
	go func() { done <- c.get("http://127.0.0.1:1/v1", "", "m") }()
	select {
	case got := <-done:
		if got != 12288 {
			t.Fatalf("got %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a stale answer waited for the probe")
	}
	close(block)
	c.wait("http://127.0.0.1:1/v1", "m")
}

// In no-wait mode (the TUI, whose Update and View must do no I/O), a
// question with no answer yet returns 0 at once, the probe runs in the
// background, and onChange is called when its answer arrives.
func TestCachedProbeNoWait(t *testing.T) {
	block := make(chan struct{})
	c := &windowCache{ttl: time.Minute, entries: map[string]*windowEntry{},
		probe: func(context.Context, string, string, string) int { <-block; return 24576 }}
	changed := make(chan struct{}, 4)
	c.setNoWait(true, func() { changed <- struct{}{} })
	if got := c.get("http://127.0.0.1:1/v1", "", "m"); got != 0 {
		t.Fatalf("no-wait miss: got %d, want 0", got)
	}
	close(block)
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("onChange was not called")
	}
	if got := c.get("http://127.0.0.1:1/v1", "", "m"); got != 24576 {
		t.Fatalf("got %d", got)
	}
}

// #310 review: a llama.cpp or LM Studio server started with an API key
// answers the probe only with it. The key goes to a local host only (the
// caller checked), as a Bearer token.
func TestProbeSendsTheAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-local" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/props" {
			_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":12288}}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if got := ProbeLocalWindow(ctx, http.DefaultClient, srv.URL+"/v1", "sk-local", "m"); got != 12288 {
		t.Fatalf("got %d, want 12288", got)
	}
	if got := ProbeLocalWindow(ctx, http.DefaultClient, srv.URL+"/v1", "", "m"); got != 0 {
		t.Fatalf("without the key: got %d", got)
	}
}
