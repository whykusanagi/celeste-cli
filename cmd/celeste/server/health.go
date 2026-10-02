package server

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
)

// completionHealth counts this process's completion calls (celeste chat,
// agent runs, celeste_content) by outcome, so celeste_status stops saying
// "ok" while every completion times out (2.0 W3).
type completionHealth struct {
	mu         sync.Mutex
	ok, failed int
	lastFailed bool
	lastErr    string
}

// record counts one call's outcome. A cancelled call (the client went
// away) is neither.
func (h *completionHealth) record(err error) {
	if h == nil || errors.Is(err, context.Canceled) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.failed++
		h.lastFailed, h.lastErr = true, err.Error()
		if len(h.lastErr) > 300 {
			h.lastErr = strings.ToValidUTF8(h.lastErr[:300], "")
		}
		return
	}
	h.ok++
	h.lastFailed = false
}

// state is celeste_status "health": "degraded" while the latest completion
// failed, else "ok".
func (h *completionHealth) state() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastFailed {
		return "degraded"
	}
	return "ok"
}

// snapshot is celeste_status "completions".
func (h *completionHealth) snapshot() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]any{"ok": h.ok, "failed": h.failed}
	if h.lastFailed {
		out["last_error"] = h.lastErr
	}
	return out
}

type healthKey struct{}

// withHealth carries the tally into an agent run (agentExecFn has no
// *Server), like withCost.
func withHealth(ctx context.Context, h *completionHealth) context.Context {
	return context.WithValue(ctx, healthKey{}, h)
}

func healthFrom(ctx context.Context) *completionHealth {
	h, _ := ctx.Value(healthKey{}).(*completionHealth)
	return h
}

// oracleStatus is celeste_status "oracle": the configured oracle, the jev_*
// modes, and every model-backed call's latency and hit rate.
func oracleStatus(cfg *config.Config) map[string]any {
	if cfg == nil {
		cfg = &config.Config{}
	}
	out := decide.Snapshot()
	out["mode"] = cfg.OracleMode()
	out["jev"] = map[string]any{"prune": cfg.JevPruneMode(), "gate": cfg.JevGateMode(), "route": cfg.JevRouteMode()}
	return out
}

// rulesStatus is celeste_status "rules": stream_rules, the watchdog mode,
// and rule-fire counts.
func rulesStatus(cfg *config.Config) map[string]any {
	if cfg == nil {
		cfg = &config.Config{}
	}
	out := rules.Snapshot()
	out["mode"] = cfg.StreamRulesMode()
	out["watchdog"] = cfg.WatchdogMode()
	return out
}
