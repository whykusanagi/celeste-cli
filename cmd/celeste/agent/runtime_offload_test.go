package agent

import (
	"testing"

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
