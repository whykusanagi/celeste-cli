package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// #144 W6b review, I1: View() ran config.Load() from disk on every render
// just to read ConfirmActions, even though the model already caches the
// active config (m.config, kept current by SetConfig and every place that
// mutates it). Reached from every render inside the running chat TUI, with
// an unwritable ~/.celeste this repeated migrateFile's warn-and-retry once
// per frame. The skills panel's confirm/auto label must reflect the cached
// config, not a fresh disk read that can disagree with it.
func TestViewUsesCachedConfigForConfirmModeWithoutReloadingFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	celesteDir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(celesteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The on-disk config disagrees with the cached one on purpose: if View()
	// still reloads from disk, it renders "Mode: auto" instead of "confirm".
	if err := os.WriteFile(filepath.Join(celesteDir, "config.json"), []byte(`{"confirm_actions": false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp(nil).WithEndpoint("openai")
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = sized.(AppModel)
	app.skills.expanded = true
	app = app.SetConfig(&config.Config{ConfirmActions: true})

	view := app.View()
	if !strings.Contains(view, "Mode: confirm") {
		t.Error("View() must render the model's cached config (ConfirmActions=true), not a fresh disk read that disagrees with it")
	}
}

// Now that View() renders the model's cached config instead of reloading
// from disk, /confirm (which loads, flips and saves its own *config.Config)
// must also refresh the model's cache — otherwise the skills panel's
// confirm/auto label would freeze at whatever it showed before the toggle
// until the app restarts.
func TestConfirmCommandUpdatesTheModelsCachedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	celesteDir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(celesteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(celesteDir, "config.json"), []byte(`{"confirm_actions": false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp(nil).WithEndpoint("openai")
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = sized.(AppModel)
	app.skills.expanded = true
	app = app.SetConfig(&config.Config{ConfirmActions: false})

	updated, _ := app.Update(SendMessageMsg{Content: "/confirm"})
	app = updated.(AppModel)

	if app.config == nil || !app.config.ConfirmActions {
		t.Fatal("/confirm must update the model's cached config, not just the one it loaded and saved locally")
	}
	if view := app.View(); !strings.Contains(view, "Mode: confirm") {
		t.Error("/confirm must take effect immediately, without an app restart")
	}
}

// /confirm reloads config.json to flip and save confirm_actions. It used to
// replace the whole session config with that reload, which dropped the
// active profile (and every field config.json does not share with it), so a
// later collections save went to config.json instead of the profile. Only
// ConfirmActions may change in the session config.
func TestConfirmCommandKeepsTheSessionConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	celesteDir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(celesteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(celesteDir, "config.json"), []byte(`{"model": "disk-model", "confirm_actions": false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp(nil).WithEndpoint("openai")
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = sized.(AppModel)
	session := &config.Config{Model: "session-model"}
	app = app.SetConfig(session)

	updated, _ := app.Update(SendMessageMsg{Content: "/confirm"})
	app = updated.(AppModel)

	if app.config != session {
		t.Fatal("/confirm replaced the session config with config.json's")
	}
	if app.config.Model != "session-model" || !app.config.ConfirmActions {
		t.Fatalf("session config = model %q confirm %v; want session-model, true", app.config.Model, app.config.ConfirmActions)
	}
}
