package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Which components each mode wires today (spec F1 setup coverage).
// F2's Setup(mode) moves every row to all-true; update the want table then.
func TestSetupCoverageToday(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".celeste", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".celeste", "skills", "hello.json"),
		[]byte(`{"name":"hello_skill","description":"x","parameters":{"type":"object"},"command":"echo hi"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}

	_, deps, err := newChatApp(cfg, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	_, tuiHasSkill := deps.registry.Get("hello_skill")

	want := map[string]bool{"tui.custom_skills": true}
	got := map[string]bool{"tui.custom_skills": tuiHasSkill}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// Agent and MCP rows are documented, not probed: neither agent.Runner nor
	// server.Server exposes its internal *tools.Registry through an exported
	// accessor (grep -n "^func (r \*Runner)" cmd/celeste/agent/*.go and
	// grep -n "^func (s \*Server)" cmd/celeste/server/*.go). server.Server
	// exposes MCP tool registration/listing, but that is a private
	// []mcp.MCPToolDef/handlers map, not the chat/agent *tools.Registry, and
	// this task must not add a production accessor just to probe it.
	// Source-read confirms: cmd/celeste/agent/runtime.go's NewRunner builds its
	// own tools.Registry via builtin.RegisterAll with a nil skill config loader
	// (comment: "Agent registry: register dev tools only (no configLoader = no
	// skill tools)"), so agent mode never registers custom JSON skills today.
	// cmd/celeste/server similarly builds a private MCP tool list through New +
	// RegisterHandlers, with no exported way to list or query any internal
	// tools.Registry from package main.
	// want table for those two rows, for the record (not asserted above because
	// there is no observable probe today):
	//   agent.custom_skills = false
	//   mcp_server.custom_skills = false
	// Hooks (grimoire) and memories are wired identically for every mode via
	// shared prompt composition, so this survey does not add a hooks/memories
	// probe row.
}
