package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testCfg() *config.Config {
	return &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model"}
}

// warnings collects what Setup (and later its hooks) report.
type warnings struct {
	mu   sync.Mutex
	list []string
}

func (w *warnings) add(s string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, s)
}

func (w *warnings) all() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.list, "\n")
}

func mustSetup(t *testing.T, mode Mode, ws string) (*Env, *warnings) {
	t.Helper()
	w := &warnings{}
	env, err := Setup(mode, testCfg(), ws, SetupOptions{Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return env, w
}

// Every component the 2.0 setup-coverage table lists, in agent mode, which
// wired only builtins and the code graph before F2.
func TestSetupAgentWiresEveryComponent(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(home, ".celeste", "skills", "hello.json"),
		`{"name":"hello_skill","description":"x","parameters":{"type":"object"},"command":"echo hi"}`)
	write(t, filepath.Join(ws, ".grimoire"), "## Bindings\n- setup-probe-binding\n")
	write(t, filepath.Join(ws, "go.mod"), "module setupprobe\n\ngo 1.26\n")
	write(t, filepath.Join(ws, "main.go"), "package main\n\nfunc main() {}\n")
	absWS, _ := filepath.Abs(ws)
	store := memories.NewStore(absWS)
	idx, _ := memories.LoadIndex(filepath.Join(store.BaseDir(), "MEMORY.md"))
	_ = idx.Add(memories.IndexEntry{Name: "probe-memory", File: "probe.md", Description: "setup probe"})
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}

	env, _ := mustSetup(t, ModeAgent, ws)
	for _, name := range []string{"read_file", "write_file", "bash", "hello_skill", "code_search"} {
		if _, ok := env.Registry.Get(name); !ok {
			t.Errorf("registry lacks %s", name)
		}
	}
	if env.ToolMode != tools.ModeAgent || env.MCP == nil || env.Indexer == nil {
		t.Fatalf("env = %+v", env)
	}
	prompt := env.SystemPrompt(PromptOptions{Contract: "CONTRACT-PROBE"}).String()
	for _, want := range []string{"CONTRACT-PROBE", "setup-probe-binding", "# Project Memories", "probe-memory", "# Code Graph", "setupprobe"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("agent system prompt lacks %q", want)
		}
	}
}

type info struct {
	name string
	ro   bool
}

func (i info) ToolName() string { return i.name }
func (i info) IsReadOnly() bool { return i.ro }

func TestSetupPermissions(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "permissions.json"),
		`{"mode":"default","always_deny":[{"tool_pattern":"bash(sudo *)","decision":"deny"}]}`)

	agentEnv, _ := mustSetup(t, ModeAgent, t.TempDir())
	if d := agentEnv.Checker.Check(info{"write_file", false}, map[string]any{"path": "x"}).Decision; d != permissions.Ask {
		t.Fatalf("agent default policy: write_file = %v, want Ask", d)
	}
	agentEnv.Trust()
	if d := agentEnv.Checker.Check(info{"write_file", false}, map[string]any{"path": "x"}).Decision; d != permissions.Allow {
		t.Fatalf("after Trust: write_file = %v, want Allow", d)
	}
	if d := agentEnv.Checker.Check(info{"bash", false}, map[string]any{"command": "sudo rm -rf /"}).Decision; d != permissions.Deny {
		t.Fatalf("after Trust: sudo bash = %v, want the deny rule to hold", d)
	}

	mcpEnv, _ := mustSetup(t, ModeMCPChat, t.TempDir())
	if d := mcpEnv.Checker.Check(info{"write_file", false}, map[string]any{"path": "x"}).Decision; d != permissions.Allow {
		t.Fatalf("MCP chat: write_file = %v, want Allow (calling the tool is the approval)", d)
	}
	if d := mcpEnv.Checker.Check(info{"bash", false}, map[string]any{"command": "sudo ls"}).Decision; d != permissions.Deny {
		t.Fatalf("MCP chat: sudo bash = %v, want Deny (#187)", d)
	}
}

// A malformed permissions.json must not silently fall back to defaults:
// under MCP-chat Trust mode that would drop the user's deny rules (#187).
func TestSetupPermissionsMalformedWarns(t *testing.T) {
	home := setupHome(t)
	path := filepath.Join(home, ".celeste", "permissions.json")
	write(t, path, "{not json")

	env, w := mustSetup(t, ModeAgent, t.TempDir())
	if !strings.Contains(w.all(), path) || !strings.Contains(w.all(), "default") {
		t.Fatalf("warnings = %q, want the malformed permissions file named and defaults reported", w.all())
	}
	if d := env.Checker.Check(info{"write_file", false}, map[string]any{"path": "x"}).Decision; d != permissions.Ask {
		t.Fatalf("write_file = %v, want the default policy (Ask) still applied", d)
	}
}

func TestSetupChatPromptMatchesChatComposition(t *testing.T) {
	setupHome(t)
	env, _ := mustSetup(t, ModeChat, t.TempDir())
	if got, want := env.SystemPrompt(PromptOptions{}).String(), prompts.Compose(prompts.ComposeOptions{Mode: prompts.ModeChat, Window: 8192, ProjectContext: env.ProjectContext, GitSnapshot: env.GitSnapshot, Memories: env.Memories}).String(); got != want {
		t.Fatalf("chat prompt differs from the TUI's composition:\n%s\n---\n%s", got, want)
	}
	if env.ToolMode != tools.ModeChat {
		t.Fatalf("ToolMode = %v", env.ToolMode)
	}
}

func TestSetupDoesNotCreateGrimoire(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	mustSetup(t, ModeChat, ws)
	if _, err := os.Stat(filepath.Join(ws, ".grimoire")); !os.IsNotExist(err) {
		t.Fatal("Setup must not create .grimoire (W4 removes auto-init)")
	}
}

func TestEnvCloseIsIdempotent(t *testing.T) {
	setupHome(t)
	env, err := Setup(ModeAgent, testCfg(), t.TempDir(), SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	env.Close()
	env.Close() // must not panic or block
}

// A code-graph update that outlived Setup's 10s wait must not make Close
// block unboundedly: Close cancels it first, so it stops instead of running
// to completion in the background. The real indexer finishes too fast on a
// small test repo to exercise this, so this stands in for it directly with
// an update that only ever stops once its context is cancelled.
func TestEnvCloseCancelsAStuckCodeGraphUpdate(t *testing.T) {
	env := &Env{opts: SetupOptions{Warn: func(string) {}}}
	ctx, cancel := context.WithCancel(context.Background())
	env.indexCancel = cancel
	env.indexing.Add(1)
	go func() {
		defer env.indexing.Done()
		<-ctx.Done() // stands in for codegraph.Indexer.UpdateWithContext
	}()

	done := make(chan struct{})
	go func() {
		env.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return promptly; it must cancel a stuck code-graph update before waiting on it")
	}
}

func TestSetupBadSkillIsAWarning(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "skills", "broken.json"), "{not json")
	_, w := mustSetup(t, ModeAgent, t.TempDir())
	if !strings.Contains(w.all(), "custom skills") {
		t.Fatalf("warnings = %q, want the broken skill reported", w.all())
	}
}

// markerMCPConfig is an MCP config whose one enabled server creates marker
// when it is started.
func markerMCPConfig(marker string) string {
	return `{"mcpServers":{"probe":{"enabled":true,"command":"sh","args":["-c",` +
		strconv.Quote("touch '"+marker+"'") + `]}}}`
}

func waitForFile(path string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A repo's MCP config must not run its command in a non-interactive run.
func TestSetupAgentSkipsWorkspaceMCPConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	setupHome(t)
	ws := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	for _, cfg := range []string{filepath.Join(ws, ".mcp.json"), filepath.Join(ws, ".celeste", "mcp.json")} {
		write(t, cfg, markerMCPConfig(marker))
	}

	_, w := mustSetup(t, ModeAgent, ws)
	if waitForFile(marker, 300*time.Millisecond) {
		t.Fatal("agent-mode Setup started a workspace MCP server")
	}
	got := w.all()
	for _, want := range []string{strconv.Quote(filepath.Join(ws, ".mcp.json")), strconv.Quote(filepath.Join(ws, ".celeste", "mcp.json")), "non-interactive", "global config"} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings lack %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "skipping repo MCP") != 1 {
		t.Errorf("want one repo-MCP warning, got:\n%s", got)
	}
}

// The same server in a home-level config still starts.
func TestSetupAgentStartsGlobalMCPConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	home := setupHome(t)
	ws := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	write(t, filepath.Join(home, ".celeste", "mcp.json"), markerMCPConfig(marker))

	_, w := mustSetup(t, ModeAgent, ws)
	if !waitForFile(marker, 5*time.Second) {
		t.Fatal("agent-mode Setup did not start a home-level MCP server")
	}
	if strings.Contains(w.all(), "repo MCP") {
		t.Errorf("unexpected repo-MCP warning:\n%s", w.all())
	}
}

// Timing notices go to SetupOptions.Notice, not Warn: they depend on the
// machine, not the configuration.
func TestSetupTimingNoticesGoToNotice(t *testing.T) {
	setupHome(t)
	origTimeout, origUpdate := codeGraphTimeout, updateCodeGraph
	t.Cleanup(func() { codeGraphTimeout, updateCodeGraph = origTimeout, origUpdate })
	codeGraphTimeout = 50 * time.Millisecond
	// The update blocks until Close cancels it, so the timeout always wins:
	// no race between a real update and the timer.
	updateCodeGraph = func(ctx context.Context, _ *codegraph.Indexer) error {
		<-ctx.Done()
		return ctx.Err()
	}
	warns, notices := &warnings{}, &warnings{}
	env, err := Setup(ModeMCPChat, testCfg(), t.TempDir(), SetupOptions{Warn: warns.add, Notice: notices.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	if !strings.Contains(notices.all(), "code graph update timed out") {
		t.Fatalf("notices = %q", notices.all())
	}
	if strings.Contains(warns.all(), "timed out") {
		t.Fatalf("a timing notice reached Warn: %q", warns.all())
	}
}

// An index that another celeste process is writing is not a failure: the
// update skips (#392) and Setup says so as a notice, not a warning.
func TestSetupBusyIndexIsANotice(t *testing.T) {
	setupHome(t)
	origUpdate := updateCodeGraph
	t.Cleanup(func() { updateCodeGraph = origUpdate })
	updateCodeGraph = func(context.Context, *codegraph.Indexer) error { return codegraph.ErrIndexBusy }
	warns, notices := &warnings{}, &warnings{}
	env, err := Setup(ModeMCPChat, testCfg(), t.TempDir(), SetupOptions{Warn: warns.add, Notice: notices.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	if !strings.Contains(notices.all(), "another celeste process") {
		t.Fatalf("notices = %q", notices.all())
	}
	if strings.Contains(warns.all(), "code graph") {
		t.Fatalf("a busy index reached Warn: %q", warns.all())
	}
}

// /grimoire and /index show the grimoire (with memories) and the code-graph
// summary on their own; ProjectContext is their join.
func TestSetupExposesGrimoireAndCodeGraphText(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".grimoire"), "## Bindings\n- GRIMOIRE-MARKER\n")
	write(t, filepath.Join(ws, "go.mod"), "module probe\n\ngo 1.26\n")
	write(t, filepath.Join(ws, "main.go"), "package main\n\nfunc main() {}\n")
	env, _ := mustSetup(t, ModeChat, ws)
	if !strings.Contains(env.GrimoireContext, "GRIMOIRE-MARKER") || strings.Contains(env.GrimoireContext, "# Code Graph") {
		t.Fatalf("GrimoireContext = %q", env.GrimoireContext)
	}
	if env.CodeGraphSummary == "" || !strings.Contains(env.ProjectContext, env.CodeGraphSummary) {
		t.Fatalf("CodeGraphSummary = %q, ProjectContext = %q", env.CodeGraphSummary, env.ProjectContext)
	}
}

// Tools an adopter adds after Setup (the chat's) count toward discovery.
func TestEnvRefreshDiscoveryCountsLateTools(t *testing.T) {
	setupHome(t)
	env, _ := mustSetup(t, ModeChat, t.TempDir())
	if env.Registry.DiscoveryMode() {
		t.Skip("discovery is already on for a bare Setup; nothing to test")
	}
	for i := env.Registry.Count(); i <= ToolDiscoveryThreshold; i++ {
		env.Registry.Register(&fakeTool{name: fmt.Sprintf("late_%d", i)})
	}
	env.RefreshDiscovery()
	if !env.Registry.DiscoveryMode() {
		t.Fatalf("%d tools and discovery still off", env.Registry.Count())
	}
}
