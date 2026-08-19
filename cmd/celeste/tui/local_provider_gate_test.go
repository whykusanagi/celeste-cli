package tui

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// The chat TUI enables its tool surface in WithEndpoint: it looks the provider
// up and only assigns skillsEnabled inside the ok branch. main.go:553-557
// derives that provider name with DetectProvider(cfg.BaseURL), so a localhost
// base_url used to resolve to "unknown", miss the branch, and leave
// skillsEnabled at its false zero value — no tools on the wire at all.
//
// This drives the same chain the real TUI does.
func TestWithEndpoint_LocalProviderEnablesSkills(t *testing.T) {
	provider := providers.DetectProvider("http://127.0.0.1:8080/v1")
	if provider != "local" {
		t.Fatalf("DetectProvider = %q, want \"local\"", provider)
	}

	app := NewApp(nil).WithEndpoint(provider)

	if !app.skillsEnabled {
		t.Error("skillsEnabled is false for a local endpoint — the tool surface stays dark")
	}
	if app.provider != "local" {
		t.Errorf("provider = %q, want \"local\"", app.provider)
	}
}

// A local server needs the model the user configured. mlx-vlm wants a full
// filesystem path and 404s on anything it cannot resolve as a HuggingFace repo
// id, so the auto-select in the same branch must not fire.
func TestWithEndpoint_LocalDoesNotAutoSelectModel(t *testing.T) {
	app := NewApp(nil).WithEndpoint("local")
	if app.model != "" {
		t.Errorf("model was auto-selected as %q; a local server's model must stay as configured", app.model)
	}
}

// The hosted path must be untouched.
func TestWithEndpoint_HostedProvidersUnchanged(t *testing.T) {
	for _, name := range []string{"openai", "sakana", "grok"} {
		app := NewApp(nil).WithEndpoint(name)
		if !app.skillsEnabled {
			t.Errorf("%s: skillsEnabled became false", name)
		}
	}
}
