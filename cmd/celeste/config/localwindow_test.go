package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// localServer serves routes (path -> JSON body) and 404s the rest.
func localServer(t *testing.T, routes map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func probe(t *testing.T, baseURL, model string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ProbeLocalWindow(ctx, http.DefaultClient, baseURL, "", model)
}

// Ollama reports a loaded model's runtime window in /api/ps; that wins
// over the model's trained maximum (#310).
func TestProbeOllamaLoadedModel(t *testing.T) {
	srv, _ := localServer(t, map[string]any{
		"GET /api/ps":    map[string]any{"models": []any{map[string]any{"name": "qwen3:14b", "model": "qwen3:14b", "context_length": 32768}}},
		"POST /api/show": map[string]any{"parameters": "temperature 0.6", "model_info": map[string]any{"qwen3.context_length": 40960}},
	})
	if got := probe(t, srv.URL+"/v1", "qwen3:14b"); got != 32768 {
		t.Fatalf("got %d, want 32768", got)
	}
	// A name without a tag is :latest.
	latest, _ := localServer(t, map[string]any{
		"GET /api/ps": map[string]any{"models": []any{map[string]any{"name": "llama3:latest", "model": "llama3:latest", "context_length": 8000}}},
	})
	if got := probe(t, latest.URL, "llama3"); got != 8000 {
		t.Fatalf("got %d, want 8000 for llama3 = llama3:latest", got)
	}
}

// A model Ollama has not loaded: its num_ctx parameter, never the trained
// maximum (Ollama runs it with its own default, often far smaller).
func TestProbeOllamaNumCtx(t *testing.T) {
	srv, _ := localServer(t, map[string]any{
		"GET /api/ps":    map[string]any{"models": []any{}},
		"POST /api/show": map[string]any{"parameters": "num_ctx                        16384\ntemperature 0.6", "model_info": map[string]any{"qwen3.context_length": 40960}},
	})
	if got := probe(t, srv.URL+"/v1", "qwen3:14b"); got != 16384 {
		t.Fatalf("got %d, want 16384", got)
	}
	srv2, _ := localServer(t, map[string]any{
		"GET /api/ps":    map[string]any{"models": []any{}},
		"POST /api/show": map[string]any{"parameters": "temperature 0.6", "model_info": map[string]any{"qwen3.context_length": 40960}},
	})
	if got := probe(t, srv2.URL+"/v1", "qwen3:14b"); got != 0 {
		t.Fatalf("got %d, want 0 (unknown) without num_ctx or a loaded model", got)
	}
}

func TestProbeLlamaCpp(t *testing.T) {
	srv, _ := localServer(t, map[string]any{
		"GET /props": map[string]any{"default_generation_settings": map[string]any{"n_ctx": 12288}},
	})
	if got := probe(t, srv.URL+"/v1", "anything"); got != 12288 {
		t.Fatalf("got %d, want 12288", got)
	}
}

func TestProbeLMStudio(t *testing.T) {
	srv, _ := localServer(t, map[string]any{
		"GET /api/v0/models/my-model": map[string]any{"state": "loaded", "loaded_context_length": 24576, "max_context_length": 131072},
	})
	if got := probe(t, srv.URL+"/v1", "my-model"); got != 24576 {
		t.Fatalf("got %d, want 24576", got)
	}
}

func TestProbeUnknownServer(t *testing.T) {
	srv, _ := localServer(t, nil)
	if got := probe(t, srv.URL+"/v1", "m"); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// ResolveContextLimit uses the installed probe for a local endpoint with
// no context_limit, and reports the window as known. An override still
// wins, a hosted endpoint is never probed, and a probe that finds nothing
// leaves the 8,192 fallback.
func TestResolveContextLimitUsesTheProbe(t *testing.T) {
	var calls atomic.Int32
	SetLocalWindowProbe(func(baseURL, apiKey, model string) int {
		calls.Add(1)
		if model == "reports" {
			return 32768
		}
		return 0
	})
	t.Cleanup(func() { SetLocalWindowProbe(nil) })
	if n, known := ResolveContextLimit("http://127.0.0.1:11434/v1", "reports", 0, ""); n != 32768 || !known {
		t.Fatalf("got %d %v", n, known)
	}
	if _, src := ResolveContextLimitSource("http://127.0.0.1:11434/v1", "reports", 0, ""); src != SourceReported {
		t.Fatalf("source %q", src)
	}
	if _, src := ResolveContextLimitSource("http://127.0.0.1:11434/v1", "silent", 0, ""); src != SourceFallback {
		t.Fatalf("source %q", src)
	}
	if _, src := ResolveContextLimitSource("https://api.anthropic.com/v1", "claude-opus-4-8", 0, ""); src != SourceModel {
		t.Fatalf("source %q", src)
	}
	if n, known := ResolveContextLimit("http://127.0.0.1:11434/v1", "silent", 0, ""); n != 8192 || known {
		t.Fatalf("got %d %v", n, known)
	}
	if n, _ := ResolveContextLimit("http://127.0.0.1:11434/v1", "reports", 4096, ""); n != 4096 {
		t.Fatalf("override lost: %d", n)
	}
	before := calls.Load()
	ResolveContextLimit("https://api.openai.com/v1", "gpt-4.1", 0, "")
	if calls.Load() != before {
		t.Fatal("a hosted endpoint was probed")
	}
}

// The caching probe asks the server once per endpoint and model while its
// answer is fresh.
func TestCachedProbeAsksOnce(t *testing.T) {
	srv, hits := localServer(t, map[string]any{
		"GET /props": map[string]any{"default_generation_settings": map[string]any{"n_ctx": 12288}},
	})
	p := newWindowCache(http.DefaultClient, time.Second)
	for range 3 {
		if got := p.get(srv.URL+"/v1", "", "m"); got != 12288 {
			t.Fatalf("got %d", got)
		}
	}
	first := hits.Load()
	p.get(srv.URL+"/v1", "", "m")
	if hits.Load() != first {
		t.Fatal("a fresh answer was asked again")
	}
}

// The probe is asked about hosts on this machine or the local network
// only, by the parsed host: a LAN server is asked, a hosted URL that
// contains "localhost" never is.
func TestResolveContextLimitProbesLocalHostsOnly(t *testing.T) {
	var asked []string
	SetLocalWindowProbe(func(baseURL, apiKey, model string) int {
		asked = append(asked, baseURL)
		return 16384
	})
	t.Cleanup(func() { SetLocalWindowProbe(nil) })
	if n, known := ResolveContextLimit("http://192.168.1.20:11434/v1", "qwen3:14b", 0, ""); n != 16384 || !known {
		t.Fatalf("LAN server: got %d %v", n, known)
	}
	for _, u := range []string{"https://localhost.evil.example/v1", "https://example.com/localhost/v1"} {
		ResolveContextLimit(u, "m", 0, "")
	}
	if len(asked) != 1 {
		t.Fatalf("probed %v, want only the LAN server", asked)
	}
}

// A LAN, bare-hostname or .local server whose probe gets no answer falls
// back to the local 8,192 window, not the model's hosted default (fugu's
// 1,000,000), and gets the local timeouts; a hosted URL whose path says
// "localhost" gets neither (#377).
func TestLocalFallbackFollowsTheHostRule(t *testing.T) {
	SetLocalWindowProbe(func(baseURL, apiKey, model string) int { return 0 })
	t.Cleanup(func() { SetLocalWindowProbe(nil) })
	for _, u := range []string{
		"http://127.0.0.1:8080/v1",
		"http://192.168.1.20:8080/v1",
		"http://10.1.2.3:8080/v1",
		"http://172.20.0.4:8080/v1",
		"http://gpu-box:8080/v1",
		"http://mac.local:1234/v1",
	} {
		n, src := ResolveContextLimitSource(u, "fugu", 0, "")
		if n != 8192 || src != SourceFallback {
			t.Errorf("%s: window %d (%s), want 8192 fallback", u, n, src)
		}
		c := &Config{BaseURL: u, Timeout: DefaultTimeoutSeconds}
		if got := c.GetTimeout(); got != LocalTimeoutSeconds*time.Second {
			t.Errorf("%s: timeout %v, want local", u, got)
		}
	}
	proxy := "https://proxy.example.com/localhost/v1"
	if n, src := ResolveContextLimitSource(proxy, "fugu", 0, ""); n == 8192 || src == SourceFallback {
		t.Errorf("%s: window %d (%s), want the model default", proxy, n, src)
	}
	c := &Config{BaseURL: proxy, Timeout: DefaultTimeoutSeconds}
	if got := c.GetTimeout(); got != DefaultTimeoutSeconds*time.Second {
		t.Errorf("%s: timeout %v, want the hosted default", proxy, got)
	}
}

// wait blocks until no probe for baseURL and model is running (tests).
func (c *windowCache) wait(baseURL, model string) {
	c.mu.Lock()
	var ch chan struct{}
	if e := c.entries[windowKey(baseURL, model)]; e != nil {
		ch = e.inflight
	}
	c.mu.Unlock()
	if ch != nil {
		<-ch
	}
}
