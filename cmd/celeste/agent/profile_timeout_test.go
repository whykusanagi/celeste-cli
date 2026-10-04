package agent

import (
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
)

func timeoutRunner(t *testing.T, cfg *config.Config, explicit time.Duration) *Runner {
	t.Helper()
	isolateHome(t)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true
	if explicit > 0 {
		opts.RequestTimeout = explicit
		opts.RequestTimeoutExplicit = true
	}
	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

// L3: `celeste agent` used its own 90 s per-request deadline and ignored the
// profile's timeout, so a local agent run died on its first request unless
// -request-timeout was passed. The profile's timeout is the stall timeout
// here too, and the turn itself is only bounded by the hard cap.
func TestAgentHonoursProfileTimeout(t *testing.T) {
	cfg := newTestConfig("http://127.0.0.1:11434/v1", "qwen3:14b")
	cfg.Timeout = 900
	r := timeoutRunner(t, cfg, 0)
	if got := r.client.GetConfig().Timeout; got != 900*time.Second {
		t.Errorf("client stall timeout = %v, want the profile's 900s", got)
	}
	if got, want := r.options.RequestTimeout, llm.MaxRequestDuration(900*time.Second); got != want {
		t.Errorf("turn deadline = %v, want the hard cap %v", got, want)
	}
}

// A local profile left at the generic default gets the local default in
// agent runs as in chat.
func TestAgentLocalDefaultTimeout(t *testing.T) {
	cfg := newTestConfig("http://127.0.0.1:11434/v1", "qwen3:14b")
	cfg.Timeout = config.DefaultTimeoutSeconds
	r := timeoutRunner(t, cfg, 0)
	if got := r.client.GetConfig().Timeout; got != config.LocalTimeoutSeconds*time.Second {
		t.Errorf("client stall timeout = %v, want %ds", got, config.LocalTimeoutSeconds)
	}
	if r.options.RequestTimeout < config.LocalTimeoutSeconds*time.Second {
		t.Errorf("turn deadline %v is shorter than the stall timeout", r.options.RequestTimeout)
	}
}

// An explicit -request-timeout bounds the turn, and the client may wait as
// long: a shorter profile timeout must not cut the turn first.
func TestAgentExplicitRequestTimeoutLiftsClient(t *testing.T) {
	cfg := newTestConfig("https://api.openai.com/v1", "gpt-4.1-nano")
	cfg.Timeout = 60
	r := timeoutRunner(t, cfg, 900*time.Second)
	if r.options.RequestTimeout != 900*time.Second {
		t.Errorf("turn deadline = %v, want the explicit 900s", r.options.RequestTimeout)
	}
	if got := r.client.GetConfig().Timeout; got < 900*time.Second {
		t.Errorf("client stall timeout = %v, want at least the explicit 900s", got)
	}
}

// A hosted model keeps failing fast on a dead connection: its stall timeout
// stays the profile's.
func TestAgentHostedKeepsProfileStall(t *testing.T) {
	cfg := newTestConfig("https://api.openai.com/v1", "gpt-4.1-nano")
	cfg.Timeout = 60
	r := timeoutRunner(t, cfg, 0)
	if got := r.client.GetConfig().Timeout; got != 60*time.Second {
		t.Errorf("client stall timeout = %v, want 60s", got)
	}
}
