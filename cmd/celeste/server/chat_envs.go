package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

const (
	// maxChatEnvs bounds the cache. The workspace argument can vary per call.
	maxChatEnvs = 4
	// chatEnvTTL replaces an Env this old, for inputs chatEnvStamp can't
	// see (ancestor-directory grimoires and hooks, git state, the code-graph
	// summary).
	chatEnvTTL = 10 * time.Minute
)

// chatEnvs caches one loop.Env per workspace for MCP chat, so a call pays
// Setup (MCP servers up to 5 s, code-graph update up to 10 s) only when the
// workspace's Env is built. Each call runs its own Loop and history on the
// shared Env.
type chatEnvs struct {
	mu      sync.Mutex
	entries map[string]*chatEnv
	closed  bool

	// Seams for tests.
	build    func(cfg *config.Config, workspace string, opts loop.SetupOptions) (*loop.Env, error)
	closeEnv func(*loop.Env)
	stamp    func(workspace string) string
	now      func() time.Time
	ttl      time.Duration
	max      int
}

// chatEnv is one cached Env. Calls hold it between acquire and release.
type chatEnv struct {
	env   *loop.Env
	err   error
	ready chan struct{} // closed when the build finished (env or err set)
	warns warnRouter

	stamp    string
	built    time.Time
	lastUsed time.Time
	inUse    int
	retired  bool // out of the cache: closed when the last call releases it
}

func newChatEnvs() *chatEnvs {
	return &chatEnvs{
		entries: map[string]*chatEnv{},
		build: func(cfg *config.Config, ws string, opts loop.SetupOptions) (*loop.Env, error) {
			return loop.Setup(loop.ModeMCPChat, cfg, ws, opts)
		},
		closeEnv: func(e *loop.Env) { e.Close() },
		stamp:    chatEnvStamp,
		now:      time.Now,
		ttl:      chatEnvTTL,
		max:      maxChatEnvs,
	}
}

// chatNotice logs Setup's timing notices (a git snapshot or code-graph
// update that ran out of time) to the server's stderr. They depend on the
// machine, so they never go into a tool result.
func chatNotice(s string) { fmt.Fprintln(os.Stderr, "celeste chat: "+s) }

// acquire returns the workspace's Env, building it on first use or when the
// cached one is stale, with sink attached for its warnings until release.
// Setup's warnings reach only the call that builds the Env: other calls
// attach after the build.
func (c *chatEnvs) acquire(cfg *config.Config, workspace string, sink *warnSink) (*chatEnv, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}
	key := filepath.Clean(abs)
	stamp := c.stamp(key)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("the server is shutting down")
	}
	var toClose []*chatEnv
	e := c.entries[key]
	if e != nil && (e.stamp != stamp || c.now().Sub(e.built) > c.ttl) {
		// Stale: replace it for this call even if others are still using
		// it, so a new deny rule or a revoked trust applies now. Running
		// calls finish on the old Env, which closes on its last release.
		toClose = append(toClose, c.retireLocked(key, e)...)
		e = nil
	}
	if e != nil {
		if e.inUse == 0 && e.env != nil && e.env.Files != nil {
			// The first call on an idle Env: staleness is judged per call.
			// Overlapping calls share the tracker (fails open only).
			e.env.Files.Reset()
		}
		e.inUse++
		e.lastUsed = c.now()
		c.mu.Unlock()
		c.closeAll(toClose)
		<-e.ready
		if e.err != nil {
			c.release(e, nil)
			return nil, e.err
		}
		e.warns.attach(sink)
		return e, nil
	}

	e = &chatEnv{ready: make(chan struct{}), stamp: stamp, built: c.now(), lastUsed: c.now(), inUse: 1}
	e.warns.attach(sink) // the building call gets Setup's warnings
	c.entries[key] = e
	toClose = append(toClose, c.evictLocked(key)...)
	c.mu.Unlock()
	c.closeAll(toClose)

	env, err := c.build(cfg, key, loop.SetupOptions{
		SessionID: fmt.Sprintf("mcp-chat-%d", time.Now().UnixNano()),
		Warn:      e.warns.warn,
		Notice:    chatNotice,
	})
	c.mu.Lock()
	e.env, e.err = env, err
	if err != nil && c.entries[key] == e {
		delete(c.entries, key)
	}
	close(e.ready)
	c.mu.Unlock()
	if err != nil {
		c.release(e, sink)
		return nil, err
	}
	return e, nil
}

// retireLocked removes e (the entry at key) from the cache. An idle entry is
// returned for closing; a busy one is closed by its last release.
func (c *chatEnvs) retireLocked(key string, e *chatEnv) []*chatEnv {
	delete(c.entries, key)
	e.retired = true
	if e.inUse == 0 {
		return []*chatEnv{e}
	}
	return nil
}

// invalidate retires workspace's Env, if cached. celeste_index rebuild calls
// it before deleting the codegraph DB the Env holds open; the next chat call
// builds a fresh Env.
func (c *chatEnvs) invalidate(workspace string) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return
	}
	key := filepath.Clean(abs)
	c.mu.Lock()
	var toClose []*chatEnv
	if e := c.entries[key]; e != nil {
		toClose = c.retireLocked(key, e)
	}
	c.mu.Unlock()
	c.closeAll(toClose)
}

// evictLocked drops least-recently-used entries (never keep) until the cache
// fits. It returns idle ones to close; busy ones retire and are closed by
// their last release.
func (c *chatEnvs) evictLocked(keep string) []*chatEnv {
	var idle []*chatEnv
	for len(c.entries) > c.max {
		oldestKey := ""
		for k, e := range c.entries {
			if k != keep && (oldestKey == "" || e.lastUsed.Before(c.entries[oldestKey].lastUsed)) {
				oldestKey = k
			}
		}
		if oldestKey == "" {
			break
		}
		idle = append(idle, c.retireLocked(oldestKey, c.entries[oldestKey])...)
	}
	return idle
}

// release ends a call's use of e. A retired Env closes with its last call.
func (c *chatEnvs) release(e *chatEnv, sink *warnSink) {
	e.warns.detach(sink)
	c.mu.Lock()
	e.inUse--
	e.lastUsed = c.now()
	closeNow := e.inUse == 0 && e.retired
	c.mu.Unlock()
	if closeNow {
		c.closeAll([]*chatEnv{e})
	}
}

// close shuts the cache down (Server.Close): idle Envs close now, busy ones
// when their last call releases them, and later acquires fail.
func (c *chatEnvs) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	var idle []*chatEnv
	for k, e := range c.entries {
		delete(c.entries, k)
		e.retired = true
		if e.inUse == 0 {
			idle = append(idle, e)
		}
	}
	c.mu.Unlock()
	c.closeAll(idle)
}

func (c *chatEnvs) closeAll(es []*chatEnv) {
	for _, e := range es {
		if e.env != nil {
			c.closeEnv(e.env)
		}
	}
}

// chatEnvStamp fingerprints (path, mtime, size) the config files Setup bakes
// into an Env, so a cached Env is replaced as soon as one changes: a new deny
// rule, hook, trust approval, skill, MCP server or grimoire edit applies from
// the next call. A directory's mtime covers files added to or
// removed from it; an edit inside one waits for chatEnvTTL.
func chatEnvStamp(ws string) string {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".celeste", "permissions.json"),
		filepath.Join(home, ".celeste", "hooks.json"),
		hooks.TrustPath(home),
		filepath.Join(home, ".celeste", "grimoire.md"),
		filepath.Join(home, ".celeste", "skills"),
		filepath.Join(ws, ".grimoire"),
		filepath.Join(ws, ".grimoire.local"),
		filepath.Join(ws, ".celeste", "grimoire"),
		filepath.Join(ws, ".celeste", "hooks.json"),
		// Not the memory index: save_memory writes it on every call, and a
		// full rebuild per save costs more than a summary a few minutes
		// stale (the TTL refreshes it; the memory tools read live).
	}
	paths = append(paths, mcp.GlobalConfigPaths(home)...)
	var b strings.Builder
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d\n", p, fi.ModTime().UnixNano(), fi.Size())
		} else {
			fmt.Fprintf(&b, "%s|-\n", p)
		}
	}
	return b.String()
}

// warnRouter delivers a shared Env's warnings (Setup's, then its hooks') to
// every call using the Env at that moment. A warning raised with no call
// attached, e.g. from a tool abandoned after its call returned, is dropped.
type warnRouter struct {
	mu    sync.Mutex
	sinks map[*warnSink]struct{}
}

func (r *warnRouter) attach(w *warnSink) {
	if w == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sinks == nil {
		r.sinks = map[*warnSink]struct{}{}
	}
	r.sinks[w] = struct{}{}
}

func (r *warnRouter) detach(w *warnSink) {
	if w == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sinks, w)
}

func (r *warnRouter) warn(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for w := range r.sinks {
		w.add(s)
	}
}

// warnSink collects one call's warnings for its tool result: an MCP call
// has no terminal. Hooks can warn from tool goroutines, hence the mutex.
type warnSink struct {
	mu   sync.Mutex
	list []string
}

func (w *warnSink) add(s string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, s)
}

// section renders the warnings for the end of the result, or "" when there
// are none, so a clean run's text is unchanged.
func (w *warnSink) section() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return formatWarnings(w.list)
}
