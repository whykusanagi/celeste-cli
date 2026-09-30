package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// cleanupChatDeps releases what newChatApp opened, in runChatTUI's defer order.
func cleanupChatDeps(t *testing.T, deps *chatDeps) {
	t.Helper()
	t.Cleanup(func() {
		if deps.adapter != nil && deps.adapter.lifeCancel != nil {
			deps.adapter.lifeCancel()
		}
		if deps.adapter != nil && deps.adapter.subMgr != nil {
			deps.adapter.subMgr.Close()
		}
		tui.CloseLogging()
		if deps.env != nil {
			deps.env.Close()
		}
	})
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
