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
var localProbe atomic.Pointer[func(baseURL, apiKey, model string) int]

// installedCache is the cache EnableLocalWindowProbe installed, for
// LocalWindowNoWait.
var installedCache atomic.Pointer[windowCache]

// SetLocalWindowProbe installs fn as the probe ResolveContextLimit asks
// about a local endpoint with no context_limit; nil removes it. fn returns
// the window in tokens, or 0 when the server did not say.
func SetLocalWindowProbe(fn func(baseURL, apiKey, model string) int) {
	installedCache.Store(nil)
	if fn == nil {
		localProbe.Store(nil)
		return
	}
	localProbe.Store(&fn)
}

// localProbeTimeout bounds one probe of a server: every route together.
const localProbeTimeout = 1500 * time.Millisecond

// localProbeTTL is how long an answer is reused before it is asked again
// (in the background): a server can be restarted with another window.
const localProbeTTL = time.Minute

// EnableLocalWindowProbe installs the caching HTTP probe.
func EnableLocalWindowProbe() {
	c := newWindowCache(&http.Client{Timeout: localProbeTimeout}, localProbeTTL)
	fn := c.get
	localProbe.Store(&fn)
	installedCache.Store(c)
}

// LocalWindowNoWait switches the installed probe to never wait (on) or
// back. The TUI turns it on once its prompt is composed: its Update and
// View must do no I/O, so an endpoint the probe has no answer for yet
// gets the fallback at once, the server is asked in the background, and
// onChange runs (on that goroutine) when an answer changes a window.
func LocalWindowNoWait(on bool, onChange func()) {
	if c := installedCache.Load(); c != nil {
		c.setNoWait(on, onChange)
	}
}

// localWindow asks the installed probe, if any.
func localWindow(baseURL, apiKey, model string) int {
	if p := localProbe.Load(); p != nil {
		return (*p)(baseURL, apiKey, model)
	}
	return 0
}

// windowCache answers for each endpoint and model from the last probe:
//   - a fresh answer is returned as it is;
//   - a stale one is returned at once and asked again in the background;
//   - with no answer yet, the caller waits for the probe (or, in no-wait
//     mode, gets 0 while it runs).
//
// Callers asking at the same moment share one probe. A positive window is
// kept until the server reports another: an Ollama model unloaded after
// its idle keep_alive reports nothing, and the window must not fall to
// the fallback (and flip the persona and tools) until it loads again.
type windowCache struct {
	ttl      time.Duration
	probe    func(ctx context.Context, baseURL, apiKey, model string) int
	noWait   atomic.Bool
	onChange atomic.Pointer[func()]

	mu      sync.Mutex
	entries map[string]*windowEntry
}

type windowEntry struct {
	window   int           // the last positive answer; 0: none yet
	checked  time.Time     // when a probe last finished; zero: never
	inflight chan struct{} // closed when the running probe finishes
}

func newWindowCache(hc *http.Client, ttl time.Duration) *windowCache {
	return &windowCache{
		ttl:     ttl,
		entries: map[string]*windowEntry{},
		probe: func(ctx context.Context, baseURL, apiKey, model string) int {
			return ProbeLocalWindow(ctx, hc, baseURL, apiKey, model)
		},
	}
}

func windowKey(baseURL, model string) string { return baseURL + "\x00" + model }

func (c *windowCache) setNoWait(on bool, onChange func()) {
	if onChange == nil {
		c.onChange.Store(nil)
	} else {
		c.onChange.Store(&onChange)
	}
	c.noWait.Store(on)
}

func (c *windowCache) get(baseURL, apiKey, model string) int {
	key := windowKey(baseURL, model)
	c.mu.Lock()
	e := c.entries[key]
	if e == nil {
		e = &windowEntry{}
		c.entries[key] = e
	}
	if !e.checked.IsZero() && time.Since(e.checked) < c.ttl {
		w := e.window
		c.mu.Unlock()
		return w
	}
	if e.inflight == nil {
		e.inflight = make(chan struct{})
		go c.refresh(e, baseURL, apiKey, model)
	}
	answered, w, ch := !e.checked.IsZero(), e.window, e.inflight
	c.mu.Unlock()
	if answered || c.noWait.Load() {
		return w
	}
	<-ch
	c.mu.Lock()
	defer c.mu.Unlock()
	return e.window
}

// refresh probes for e and records the answer.
func (c *windowCache) refresh(e *windowEntry, baseURL, apiKey, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), localProbeTimeout)
	n := c.probe(ctx, baseURL, apiKey, model)
	cancel()
	c.mu.Lock()
	changed := n > 0 && n != e.window
	if changed {
		e.window = n
	}
	e.checked = time.Now()
	close(e.inflight)
	e.inflight = nil
	c.mu.Unlock()
	if changed {
		if f := c.onChange.Load(); f != nil {
			(*f)()
		}
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

// ProbeLocalWindow asks the server at baseURL (an OpenAI-compatible base,
// usually ending in /v1) for the context window it runs model with, and
// returns 0 when it does not say. apiKey, when set, is sent as a Bearer
// token (a llama.cpp or LM Studio server started with a key answers only
// with it); callers send it to a local host only. It tries Ollama's running models and the
// model's num_ctx, llama.cpp's /props, then LM Studio's model entry. A
// model's trained maximum (Ollama's model_info, LM Studio's
// max_context_length) is never used: the server runs it with less.
func ProbeLocalWindow(ctx context.Context, hc *http.Client, baseURL, apiKey, model string) int {
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
		if getJSON(ctx, hc, apiKey, http.MethodGet, root+"/api/ps", nil, &ps) {
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
		if getJSON(ctx, hc, apiKey, http.MethodPost, root+"/api/show", body, &show) {
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
	if getJSON(ctx, hc, apiKey, http.MethodGet, root+"/props", nil, &props) {
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
		if getJSON(ctx, hc, apiKey, http.MethodGet, root+"/api/v0/models/"+url.PathEscape(model), nil, &lms) && lms.Loaded > 0 {
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
func getJSON(ctx context.Context, hc *http.Client, apiKey, method, u string, body []byte, out any) bool {
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
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
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
