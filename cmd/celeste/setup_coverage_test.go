package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
)

// Which components each mode wires (spec F1 setup coverage, F2 target).
// F2a moved the agent rows to true through loop.Setup (F2a plan Task 9, an
// intentional flip of the F1 "agent.custom_skills = false" row); F2b/F2d move
// the MCP and TUI rows.
func TestSetupCoverage(t *testing.T) {
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

	agentWS := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentWS, "go.mod"), []byte("module coverageprobe\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentWS, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	absWS, _ := filepath.Abs(agentWS)
	store := memories.NewStore(absWS)
	idx, _ := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md"))
	_ = idx.Add(memories.IndexEntry{Name: "coverage-memory", File: "c.md", Description: "probe"})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	opts := agent.DefaultOptions()
	opts.Workspace = agentWS
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
		t.Fatal("agent probe made no provider requests")
	}
	names := toolNames(requests[0].Body)
	if !hasString(names, "read_file") {
		t.Fatalf("agent probe tools = %v, want builtin positive control read_file", names)
	}
	system := systemMessage(requests[0].Body)

	want := map[string]bool{
		"tui.custom_skills":        true,
		"agent.custom_skills":      true, // F2a: loop.Setup
		"agent.memories":           true, // F2a: loop.Setup
		"agent.code_graph_summary": true, // F2a: loop.Setup
	}
	got := map[string]bool{
		"tui.custom_skills":        tuiHasSkill,
		"agent.custom_skills":      hasString(names, "hello_skill"),
		"agent.memories":           strings.Contains(system, "coverage-memory"),
		"agent.code_graph_summary": strings.Contains(system, "# Code Graph"),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	// Documented, not probed here:
	//   agent.hooks = true (F2a Task 9; covered by agent TestAgentRunsGlobalHooks)
	//   agent.mcp_clients = true (loop.Setup; covered by loop TestSetupAgentWiresEveryComponent)
	//   mcp_server.* = false until F2b adopts loop.Setup
}

func systemMessage(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "system" {
			s, _ := mm["content"].(string)
			return s
		}
	}
	return ""
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
