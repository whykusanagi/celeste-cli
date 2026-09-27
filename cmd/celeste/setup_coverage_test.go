package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
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

	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	opts := agent.DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.EnablePlanning = false
	opts.RequireVerification = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	agentCfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	r, err := agent.NewRunner(agentCfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if _, err := r.RunGoal(context.Background(), "say ok"); err != nil {
		t.Fatal(err)
	}
	requests := srv.Requests()
	if len(requests) == 0 {
		t.Fatal("agent.custom_skills probe made no provider requests")
	}
	names := toolNames(requests[0].Body)
	if !hasString(names, "read_file") {
		t.Fatalf("agent.custom_skills probe tools = %v, want builtin positive control read_file", names)
	}
	agentHasSkill := hasString(names, "hello_skill")

	want := map[string]bool{"tui.custom_skills": true, "agent.custom_skills": false}
	got := map[string]bool{"tui.custom_skills": tuiHasSkill, "agent.custom_skills": agentHasSkill}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	// Agent mode is probed by running a fake-provider round trip with the same
	// HOME used above, then inspecting the first OpenAI request's tools list.
	// Source-read confirms: cmd/celeste/agent/runtime.go's NewRunner builds its
	// own tools.Registry via builtin.RegisterAll with a nil skill config loader
	// (comment: "Agent registry: register dev tools only (no configLoader = no
	// skill tools)"), so agent mode never registers custom JSON skills today.
	//
	// The MCP row is documented, not probed: server.Server exposes MCP tool
	// registration/listing, but that is a private []mcp.MCPToolDef/handlers map,
	// not the chat/agent *tools.Registry, and this task must not add a
	// production accessor just to probe it. cmd/celeste/server similarly builds
	// a private MCP tool list through New + RegisterHandlers, with no exported
	// way to list or query any internal tools.Registry from package main.
	// want table for that row, for the record (not asserted above because there
	// is no observable probe today):
	//   mcp_server.custom_skills = false
	// Hooks (grimoire) and memories are wired identically for every mode via
	// shared prompt composition, so this survey does not add a hooks/memories
	// probe row.
}

func toolNames(body map[string]any) []string {
	tools, ok := body["tools"].([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		function, ok := toolMap["function"].(map[string]any)
		if !ok {
			continue
		}
		name, ok := function["name"].(string)
		if !ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
