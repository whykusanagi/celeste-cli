package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A local server's context window is whatever it was started with, and
// the model name says nothing about it, so ResolveContextLimit guessed
// 8,192 for every local endpoint (#201). Most servers say what they run
// with: Ollama (/api/ps for a loaded model, num_ctx in /api/show),
// llama.cpp (/props) and LM Studio (/api/v0/models/{id}). When one does,
// that is the window (#310); when none does, the guess stays 8,192, small
// enough never to overflow a server started with its defaults.

// localProbe is the installed probe (nil: none). The binary installs one
// at startup (EnableLocalWindowProbe); tests, which talk to fake servers
// on 127.0.0.1, install their own or none.
var localProbe atomic.Pointer[func(baseURL, model string) int]

// SetLocalWindowProbe installs fn as the probe ResolveContextLimit asks
// about a local endpoint with no context_limit; nil removes it. fn returns
// the window in tokens, or 0 when the server did not say.
func SetLocalWindowProbe(fn func(baseURL, model string) int) {
	if fn == nil {
		localProbe.Store(nil)
		return
	}
	localProbe.Store(&fn)
}

// localProbeTimeout bounds one probe of a server: every route together.
const localProbeTimeout = 1500 * time.Millisecond

// localProbeTTL is how long an answer is reused: a server can be restarted
// with another window, and a model loads after its first request.
const localProbeTTL = time.Minute

// EnableLocalWindowProbe installs the caching HTTP probe.
func EnableLocalWindowProbe() {
	SetLocalWindowProbe(newCachedProbe(&http.Client{Timeout: localProbeTimeout}, localProbeTTL))
}

// localWindow asks the installed probe, if any.
func localWindow(baseURL, model string) int {
	if p := localProbe.Load(); p != nil {
		return (*p)(baseURL, model)
	}
	return 0
}

type probeAnswer struct {
	window int
	at     time.Time
}

// newCachedProbe is a probe that asks a server at most once per ttl for a
// given endpoint and model.
func newCachedProbe(hc *http.Client, ttl time.Duration) func(baseURL, model string) int {
	var mu sync.Mutex
	cache := map[string]probeAnswer{}
	return func(baseURL, model string) int {
		key := baseURL + "\x00" + model
		mu.Lock()
		a, ok := cache[key]
		mu.Unlock()
		if ok && time.Since(a.at) < ttl {
			return a.window
		}
		ctx, cancel := context.WithTimeout(context.Background(), localProbeTimeout)
		defer cancel()
		n := ProbeLocalWindow(ctx, hc, baseURL, model)
		mu.Lock()
		cache[key] = probeAnswer{window: n, at: time.Now()}
		mu.Unlock()
		return n
	}
}

// ProbeLocalWindow asks the server at baseURL (an OpenAI-compatible base,
// usually ending in /v1) for the context window it runs model with, and
// returns 0 when it does not say. It tries Ollama's running models and the
// model's num_ctx, llama.cpp's /props, then LM Studio's model entry. A
// model's trained maximum (Ollama's model_info, LM Studio's
// max_context_length) is never used: the server runs it with less.
func ProbeLocalWindow(ctx context.Context, hc *http.Client, baseURL, model string) int {
	root := serverRoot(baseURL)
	if root == "" {
		return 0
	}
	if model != "" {
		var ps struct {
			Models []struct {
				Name          string `json:"name"`
				Model         string `json:"model"`
				ContextLength int    `json:"context_length"`
			} `json:"models"`
		}
		if getJSON(ctx, hc, http.MethodGet, root+"/api/ps", nil, &ps) {
			for _, m := range ps.Models {
				if (sameOllamaModel(m.Name, model) || sameOllamaModel(m.Model, model)) && m.ContextLength > 0 {
					return m.ContextLength
				}
			}
		}
		var show struct {
			Parameters string `json:"parameters"`
		}
		body, _ := json.Marshal(map[string]string{"model": model})
		if getJSON(ctx, hc, http.MethodPost, root+"/api/show", body, &show) {
			if n := numCtx(show.Parameters); n > 0 {
				return n
			}
		}
	}
	var props struct {
		NCtx     int `json:"n_ctx"`
		Settings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if getJSON(ctx, hc, http.MethodGet, root+"/props", nil, &props) {
		if props.Settings.NCtx > 0 {
			return props.Settings.NCtx
		}
		if props.NCtx > 0 {
			return props.NCtx
		}
	}
	if model != "" {
		var lms struct {
			Loaded int `json:"loaded_context_length"`
		}
		if getJSON(ctx, hc, http.MethodGet, root+"/api/v0/models/"+url.PathEscape(model), nil, &lms) && lms.Loaded > 0 {
			return lms.Loaded
		}
	}
	return 0
}

// sameOllamaModel reports whether Ollama's name a is model: a name without
// a tag is the model's :latest.
func sameOllamaModel(a, model string) bool {
	if !strings.Contains(model, ":") {
		model += ":latest"
	}
	if !strings.Contains(a, ":") {
		a += ":latest"
	}
	return a == model
}

var numCtxRE = regexp.MustCompile(`(?m)^\s*num_ctx\s+(\d+)\s*$`)

// numCtx reads num_ctx from Ollama's parameters text; 0 when absent.
func numCtx(params string) int {
	m := numCtxRE.FindStringSubmatch(params)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// serverRoot is baseURL without a trailing slash or /v1.
func serverRoot(baseURL string) string {
	root := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	root = strings.TrimSuffix(root, "/v1")
	return strings.TrimRight(root, "/")
}

// getJSON sends one request and decodes a 200 response into out; false on
// any failure. Bodies are capped at 1 MiB.
func getJSON(ctx context.Context, hc *http.Client, method, u string, body []byte, out any) bool {
	if ctx.Err() != nil {
		return false
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out) == nil
}
