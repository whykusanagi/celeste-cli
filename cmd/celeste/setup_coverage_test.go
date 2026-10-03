package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Which components each mode wires (spec F1 setup coverage, F2 target).
// F2a moved the agent rows through loop.Setup, F2b the MCP rows; F2d Task 7
// (Flip 2): every row is true for the chat UI, agent runs and MCP chat.
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
	writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventSessionStart, "", "context", "COVERAGE-HOOK"))
	marker := filepath.Join(t.TempDir(), "mcp-started")
	probeMCP := runtime.GOOS != "windows" // the probe server is `sh -c touch`
	if probeMCP {
		cfg := `{"mcpServers":{"probe":{"enabled":true,"command":"sh","args":["-c",` +
			strconv.Quote("touch '"+marker+"'") + `]}}}`
		if err := os.WriteFile(filepath.Join(home, ".celeste", "mcp.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws := coverageWorkspace(t)
	mcpStarted := func() bool {
		if !probeMCP {
			return true // documented: loop TestSetupAgentStartsGlobalMCPConfig
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(marker); err == nil {
				_ = os.Remove(marker)
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	got := map[string]bool{}

	// Chat UI: the real newChatApp and one real request.
	tuiSrv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	app, deps, err := newChatApp(&config.Config{APIKey: "k", BaseURL: tuiSrv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: 1_000_000}, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	got["tui.mcp_clients"] = mcpStarted()
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hi"}},
		func(m tea.Model) bool { return lastAssistant(m) == "ok" && turnIdle(m) }, 30*time.Second)
	sys := systemMessage(tuiSrv.Requests()[0].Body)
	_, got["tui.custom_skills"] = deps.registry.Get("hello_skill")
	got["tui.hooks"] = strings.Contains(sys, "COVERAGE-HOOK")
	got["tui.memories"] = strings.Contains(sys, "coverage-memory")
	got["tui.code_graph_summary"] = strings.Contains(sys, "# Code Graph")
	got["tui.grimoire"] = strings.Contains(sys, "COVERAGE-GRIMOIRE")
	got["tui.context_files"] = strings.Contains(sys, "COVERAGE-AGENTS")

	// Agent: the real runner.
	agentSrv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	opts := agent.DefaultOptions()
	opts.Workspace = ws
	opts.EnablePlanning = false
	opts.RequireVerification = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	r, err := agent.NewRunner(&config.Config{APIKey: "k", BaseURL: agentSrv.BaseURL(), Model: "fake-model", Timeout: 10}, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	got["agent.mcp_clients"] = mcpStarted()
	if _, err := r.RunGoal(context.Background(), "say ok"); err != nil {
		t.Fatal(err)
	}
	body := agentSrv.Requests()[0].Body
	sys = systemMessage(body)
	got["agent.custom_skills"] = hasString(toolNames(body), "hello_skill")
	got["agent.hooks"] = strings.Contains(sys, "COVERAGE-HOOK")
	got["agent.memories"] = strings.Contains(sys, "coverage-memory")
	got["agent.code_graph_summary"] = strings.Contains(sys, "# Code Graph")
	got["agent.grimoire"] = strings.Contains(sys, "COVERAGE-GRIMOIRE")
	got["agent.context_files"] = strings.Contains(sys, "COVERAGE-AGENTS")

	// MCP chat: the Env the server builds for mode:"chat" (F2b; the server
	// package's TestMCPChatSetupCoverage probes it end to end).
	env, err := loop.Setup(loop.ModeMCPChat, &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}, ws, loop.SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	got["mcp.mcp_clients"] = mcpStarted()
	_, got["mcp.custom_skills"] = env.Registry.Get("hello_skill")
	got["mcp.hooks"] = strings.Contains(env.SessionStartContext(context.Background(), "startup"), "COVERAGE-HOOK")
	// Memories follow git in the prompt, outside ProjectContext (W5 ruling 8).
	got["mcp.memories"] = strings.Contains(env.SystemPrompt(loop.PromptOptions{}).String(), "coverage-memory")
	got["mcp.code_graph_summary"] = strings.Contains(env.ProjectContext, "# Code Graph")
	got["mcp.grimoire"] = strings.Contains(env.ProjectContext, "COVERAGE-GRIMOIRE")
	got["mcp.context_files"] = strings.Contains(env.ProjectContext, "COVERAGE-AGENTS")

	for _, mode := range []string{"tui", "agent", "mcp"} {
		for _, row := range []string{"custom_skills", "hooks", "mcp_clients", "memories", "code_graph_summary", "grimoire", "context_files"} {
			if k := mode + "." + row; !got[k] {
				t.Errorf("%s = false, want true", k)
			}
		}
	}
}

// coverageWorkspace is a Go module with a grimoire, an AGENTS.md and one
// project memory.
func coverageWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":    "module coverageprobe\n\ngo 1.26\n",
		"main.go":   "package main\n\nfunc main() {}\n",
		".grimoire": "# Coverage\n\n## Bindings\n- COVERAGE-GRIMOIRE\n",
		"AGENTS.md": "COVERAGE-AGENTS\n",
	} {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	absWS, _ := filepath.Abs(ws)
	store := memories.NewStore(absWS)
	idx, _ := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md"))
	_ = idx.Add(memories.IndexEntry{Name: "coverage-memory", File: "c.md", Description: "probe"})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}
	return ws
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
