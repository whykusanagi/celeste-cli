package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
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
		if deps.indexer != nil {
			deps.indexer.Close()
		}
		_ = deps.mcpManager.Stop()
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
