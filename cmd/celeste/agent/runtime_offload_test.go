package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func newTestConfig(baseURL, model string) *config.Config {
	return &config.Config{
		BaseURL: baseURL,
		Model:   model,
		APIKey:  "test-key",
	}
}

func TestNewRunnerDisablesLocalPlanningForConductor(t *testing.T) {
	cfg := newTestConfig("https://api.sakana.ai/v1", "fugu-ultra")
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true
	if !opts.EnablePlanning {
		t.Fatal("precondition: DefaultOptions should enable planning")
	}
	opts.RequireVerification = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if r.options.EnablePlanning {
		t.Error("EnablePlanning = true, want false for fugu-ultra")
	}
	if r.options.RequireVerification {
		t.Error("RequireVerification = true, want false for fugu-ultra")
	}
}

func TestNewRunnerKeepsLocalPlanningForPlainModel(t *testing.T) {
	cfg := newTestConfig("https://api.openai.com/v1", "gpt-4.1-nano")
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if !r.options.EnablePlanning {
		t.Error("EnablePlanning = false, want true for a plain model")
	}
}

func TestNewRunnerUsesAgentModelOverride(t *testing.T) {
	// Options.Model is the model-router seam and wins over cfg.Model.
	cfg := newTestConfig("https://api.sakana.ai/v1", "fugu")
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true
	opts.Model = "gpt-4.1-nano" // a plain model explicitly selected for this run

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if !r.options.EnablePlanning {
		t.Error("EnablePlanning = false, want true — Options.Model overrides cfg.Model")
	}
}

func TestNewRunnerUsesClientModelForPlannerDecision(t *testing.T) {
	// cfg.Model is the model the LLM client actually talks to (NewRunner's own
	// resolution a few lines below the derivation). cfg.AgentModel is a plain
	// model that cfg.ResolveAgentModel() would prefer if it were consulted
	// here. The planner decision must follow the model the run actually
	// calls — cfg.Model, since options.Model is unset — or a fugu run with an
	// AgentModel override left EnablePlanning on, leaving two planners active.
	cfg := &config.Config{
		BaseURL:    "https://api.sakana.ai/v1",
		Model:      "fugu-ultra",
		AgentModel: "gpt-4.1-nano",
		APIKey:     "test-key",
	}
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if r.options.EnablePlanning {
		t.Error("EnablePlanning = true, want false — the run talks to fugu-ultra (cfg.Model), not cfg.AgentModel")
	}
}

func TestNewRunnerHonoursExplicitPlannerFlag(t *testing.T) {
	cfg := newTestConfig("https://api.sakana.ai/v1", "fugu-ultra")
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true
	opts.EnablePlanning = true
	opts.PlanningExplicit = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if !r.options.EnablePlanning {
		t.Error("EnablePlanning = false, want true — an explicit -planner must win")
	}
}

func TestNormalizeStateOptionsPreservesDisabledPlanning(t *testing.T) {
	// A run checkpointed in orchestrated mode: both flags deliberately false.
	state := &RunState{
		Options: Options{
			Workspace:           "/workspace",
			EnablePlanning:      false,
			RequireVerification: false,
		},
	}
	// Caller defaults, where both are true.
	fallback := DefaultOptions()
	fallback.RequireVerification = true

	normalizeStateOptions(state, fallback)

	if state.Options.EnablePlanning {
		t.Error("EnablePlanning = true after resume, want false — the checkpoint decided this")
	}
	if state.Options.RequireVerification {
		t.Error("RequireVerification = true after resume, want false — the checkpoint decided this")
	}
}

func TestNormalizeStateOptionsStillFillsAbsentFields(t *testing.T) {
	// The fix must be scoped to the two decision flags; everything else in
	// normalizeStateOptions keeps filling from the fallback.
	state := &RunState{Options: Options{}}
	fallback := DefaultOptions()
	fallback.Workspace = "/fallback-workspace"
	fallback.ArtifactDir = "/artifacts"
	fallback.VerificationCommands = []string{"go test ./..."}
	fallback.EmitArtifacts = true

	normalizeStateOptions(state, fallback)

	if state.Options.Workspace != "/fallback-workspace" {
		t.Errorf("Workspace = %q, want the fallback", state.Options.Workspace)
	}
	if state.Options.ArtifactDir != "/artifacts" {
		t.Errorf("ArtifactDir = %q, want the fallback", state.Options.ArtifactDir)
	}
	if len(state.Options.VerificationCommands) != 1 {
		t.Errorf("VerificationCommands = %v, want the fallback", state.Options.VerificationCommands)
	}
	if !state.Options.EmitArtifacts {
		t.Error("EmitArtifacts = false, want the fallback")
	}
}

// Issue #113 had two complaints. The retry storm was fixed in #122, but the
// second — "ends in the bare, uninformative error context deadline exceeded" —
// survived on the AGENT path. withRetry's actionable message never fires there
// because the agent sets its own per-turn deadline, so base.Err() is non-nil and
// the guard returns the raw error untouched. This annotates it at the agent
// layer, naming the flag that actually governs it.
func TestAnnotateTurnTimeout(t *testing.T) {
	raw := context.DeadlineExceeded

	timeout := 90 * time.Second
	got := annotateTurnTimeout(raw, true, timeout)
	// Assert against the duration's own rendering rather than a hardcoded string:
	// Go formats 90s as "1m30s", and pinning the literal tests the formatter.
	if !strings.Contains(got.Error(), timeout.String()) {
		t.Errorf("message should name the exceeded timeout %s, got: %v", timeout, got)
	}
	if !strings.Contains(got.Error(), "-request-timeout") {
		t.Errorf("message should name the flag that raises it, got: %v", got)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Error("wrapping must preserve errors.Is(context.DeadlineExceeded)")
	}

	// Not our deadline (parent cancelled, or a genuine transport failure):
	// leave the error exactly as-is so we never mislabel someone else's fault.
	other := errors.New("connection reset by peer")
	if got := annotateTurnTimeout(other, false, 90*time.Second); got.Error() != other.Error() {
		t.Errorf("non-timeout error must pass through unchanged, got: %v", got)
	}
}

// A conductor's fan-out width is chosen per request, so its latency is variable
// by design. 90s is demonstrably too low: a real review prompt through MCP agent
// mode died at that ceiling twice. Give conductors headroom by default, while
// leaving an explicitly-passed -request-timeout authoritative.
func TestNewRunnerGivesConductorsLongerTimeout(t *testing.T) {
	base := DefaultOptions().RequestTimeout

	cfg := newTestConfig("https://api.sakana.ai/v1", "fugu-ultra")
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	if r.options.RequestTimeout <= base {
		t.Errorf("conductor RequestTimeout = %v, want more than the %v default", r.options.RequestTimeout, base)
	}

	// A plain model keeps the default.
	plainCfg := newTestConfig("https://api.openai.com/v1", "gpt-4.1-nano")
	popts := DefaultOptions()
	popts.Workspace = t.TempDir()
	popts.DisableCheckpoints = true
	pr, err := NewRunner(plainCfg, popts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer pr.Close()
	if pr.options.RequestTimeout != base {
		t.Errorf("plain-model RequestTimeout = %v, want the %v default", pr.options.RequestTimeout, base)
	}

	// An explicit -request-timeout wins over the conductor bump.
	expCfg := newTestConfig("https://api.sakana.ai/v1", "fugu-ultra")
	eopts := DefaultOptions()
	eopts.Workspace = t.TempDir()
	eopts.DisableCheckpoints = true
	eopts.RequestTimeout = 45 * time.Second
	eopts.RequestTimeoutExplicit = true
	er, err := NewRunner(expCfg, eopts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer er.Close()
	if er.options.RequestTimeout != 45*time.Second {
		t.Errorf("explicit RequestTimeout = %v, want 45s untouched", er.options.RequestTimeout)
	}
}

// The conductor bump has to cover BOTH deadlines or it does nothing. The agent's
// per-turn context and the LLM client's per-attempt deadline (cfg.Timeout) are
// independent, and the tighter one wins. Raising only Options.RequestTimeout was
// moot: a real workload still died at the client's 90s, reporting the chat-path
// message ("request exceeded the 1m30s per-request timeout").
func TestNewRunnerRaisesClientTimeoutForConductor(t *testing.T) {
	cfg := newTestConfig("https://api.sakana.ai/v1", "fugu-ultra")
	cfg.Timeout = 90 // the sakana template's value, in seconds

	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	got := r.client.GetConfig().Timeout
	if got < conductorRequestTimeout {
		t.Errorf("client timeout = %v, want at least the conductor floor %v — "+
			"otherwise the client deadline fires first and the agent bump is moot",
			got, conductorRequestTimeout)
	}
}

func TestNewRunnerLeavesPlainModelClientTimeout(t *testing.T) {
	cfg := newTestConfig("https://api.openai.com/v1", "gpt-4.1-nano")
	cfg.Timeout = 60

	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.DisableCheckpoints = true

	r, err := NewRunner(cfg, opts, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	if got := r.client.GetConfig().Timeout; got != 60*time.Second {
		t.Errorf("plain-model client timeout = %v, want 60s untouched", got)
	}
}
