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
	// cfg reaches paths that save it (collections, /voice): it must keep
	// what the user wrote.
	if cfg.Model != "venice-uncensored" {
		t.Errorf("the caller's config changed to %q", cfg.Model)
	}
}

// M5: a switch to an endpoint with no named profile must not carry the old
// provider's models (a false "no longer serves" note), nor change the
// config the chat started with.
func TestSwitchEndpointFallbackDropsTheOldProvidersModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "https://api.sakana.ai/v1", Model: "fugu", AgentModel: "fugu-ultra", Timeout: 10}
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu"}, {ID: "fugu-ultra"}})()
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	if err := deps.adapter.SwitchEndpoint("grok"); err != nil {
		t.Fatal(err)
	}
	ep := deps.adapter.ActiveEndpoint()
	if ep.Provider != "grok" || ep.Model != "" || ep.AgentModel != "" {
		t.Errorf("after the fallback switch: %+v", ep)
	}
	if cfg.Model != "fugu" || cfg.BaseURL != "https://api.sakana.ai/v1" {
		t.Errorf("the switch changed the startup config: %+v", cfg)
	}
}

// 6: the agent and small models follow the endpoint: a switch re-resolves
// them from memory, and RefreshServedModels does after a catalog loads.
func TestSwitchEndpointResolvesAgentAndSmallModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeNamed(t, home, "venice", `{"api_key":"k","base_url":"https://api.venice.ai/api/v1","model":"venice-uncensored-1-2","agent_model":"gone","small_model":"gone-too"}`)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)

	if err := deps.adapter.SwitchEndpoint("venice"); err != nil {
		t.Fatal(err)
	}
	if ep := deps.adapter.ActiveEndpoint(); ep.AgentModel != "gone" {
		t.Fatalf("with no catalog yet the agent model is kept: %+v", ep)
	}
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	deps.adapter.RefreshServedModels()
	ac := deps.adapter.currentAgentConfig()
	if ac.AgentModel != "venice-uncensored-1-2" || ac.SmallModel != "venice-uncensored-1-2" {
		t.Errorf("agent=%q small=%q", ac.AgentModel, ac.SmallModel)
	}
}

func writeNamed(t *testing.T, home, name, body string) {
	t.Helper()
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config."+name+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 6: a resumed session's profile has its agent and small models resolved.
func TestRestoreEndpointResolvesAgentModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeNamed(t, home, "venice", `{"api_key":"k","base_url":"https://api.venice.ai/api/v1","model":"venice-uncensored-1-2","agent_model":"gone"}`)
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	_, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	s := &config.Session{}
	s.SetEndpoint("venice")
	restoreEndpoint(tui.NewApp(deps.adapter), cfg, deps.adapter, config.NewSessionManager(), s)
	if got := deps.adapter.baseConfig.AgentModel; got != "venice-uncensored-1-2" {
		t.Errorf("agent model = %q", got)
	}
}
