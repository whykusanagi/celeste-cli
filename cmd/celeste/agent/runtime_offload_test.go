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
