package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// cleanupChatDeps releases what newChatApp opened, in runChatTUI's defer order.
func cleanupChatDeps(t *testing.T, deps *chatDeps) {
	t.Helper()
	t.Cleanup(func() {
		if deps.adapter != nil {
			deps.adapter.shutdown(10 * time.Second)
		}
		if deps.adapter != nil && deps.adapter.subMgr != nil {
			deps.adapter.subMgr.Close()
		}
		tui.CloseLogging()
		if deps.env != nil {
			deps.env.Close()
		}
		if deps.restoreMigrationWarn != nil {
			deps.restoreMigrationWarn()
		}
	})
}

// #144 W6b review, I1(b): once the chat is running, a migration note (from a
// later /endpoint or SwitchEndpoint load of a different profile) must reach
// the log, not stderr — stderr is inside the alt screen by then.
func TestNewChatAppRoutesMigrationWarnToLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)

	config.MigrationWarn("a migration note while the chat is running")

	logPath := tui.GetLogPath()
	if logPath == "" {
		t.Fatal("no log path after newChatApp")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a migration note while the chat is running") {
		t.Error("MigrationWarn must reach the tui log once the chat is running, not stderr")
	}
}

func TestNewChatAppBuildsWithoutProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	app, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	if deps.registry == nil || deps.adapter == nil {
		t.Fatal("newChatApp returned incomplete deps")
	}
	if app.View() == "" {
		t.Log("empty initial view is fine before a WindowSizeMsg")
	}
}

// The chat's registry is loop.Setup's plus the TUI-only tools, and the
// discovery threshold counts them (2.0 F2d).
func TestNewChatAppBuildsOnLoopSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	if deps.env == nil || deps.env.Mode != loop.ModeChat || deps.registry != deps.env.Registry {
		t.Fatalf("deps.env = %+v; the chat must use Setup's registry", deps.env)
	}
	for _, name := range []string{"spawn_agent", "post_message", "read_file", "tarot_reading"} {
		if _, ok := deps.registry.Get(name); !ok {
			t.Errorf("chat registry lacks %s", name)
		}
	}
	if want := deps.registry.Count() > loop.ToolDiscoveryThreshold; deps.registry.DiscoveryMode() != want {
		t.Errorf("discovery = %v with %d tools, want %v", deps.registry.DiscoveryMode(), deps.registry.Count(), want)
	}
}

// A custom skill named like a chat-only tool still wins, as before F2d.
func TestNewChatAppCustomSkillBeatsChatOnlyTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skills := filepath.Join(home, ".celeste", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	def := `{"name":"tarot_reading","description":"my own tarot","parameters":{"type":"object"},"command":"echo hi"}`
	if err := os.WriteFile(filepath.Join(skills, "tarot.json"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	got, ok := deps.registry.Get("tarot_reading")
	if !ok || got.Description() != "my own tarot" {
		t.Fatalf("tarot_reading = %v; the custom skill must win", got)
	}
	if _, ok := deps.registry.Get("get_weather"); !ok {
		t.Error("the other config-backed tools must still register")
	}
}

// Warnings raised while the chat starts are collected for the chat (the
// alt screen hides stderr); later ones go to hookNotify.
func TestChatWarnSinkCollectsThenForwards(t *testing.T) {
	var got []string
	notify := func(s string) { got = append(got, s) }
	hookNotify.Store(&notify)
	t.Cleanup(func() { hookNotify.Store(nil) })
	s := newChatWarnSink()
	s.warn("during setup")
	if loaded := s.done(); len(loaded) != 1 || loaded[0] != "during setup" {
		t.Fatalf("done() = %v", loaded)
	}
	s.warn("later")
	if len(got) != 1 || got[0] != "later" {
		t.Fatalf("hookNotify got %v, want [later]", got)
	}
}

// The chat starts on the model the provider serves: a retired configured
// model is replaced before the TUI starts, the chat says so, and the config
// file is not rewritten.
func TestNewChatAppResolvesRetiredModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	yes := true
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true, Tools: &yes}})()

	cfg := &config.Config{APIKey: "k", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored", Timeout: 10}
	app, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	if got := deps.adapter.client.GetConfig().Model; got != "venice-uncensored-1-2" {
		t.Errorf("client model = %q, want venice-uncensored-1-2", got)
	}
	if ep := deps.adapter.ActiveEndpoint(); ep.Provider != "venice" || ep.Model != "venice-uncensored-1-2" {
		t.Errorf("ActiveEndpoint = %+v", ep)
	}
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 300, Height: 80})
	if view := sized.View(); !strings.Contains(view, "no longer serves venice-uncensored") {
		t.Errorf("the chat does not show the model note:\n%s", view)
	}
	if _, err := os.Stat(filepath.Join(home, ".celeste", "config.json")); err == nil {
		t.Error("resolution wrote a config file")
	}
}
