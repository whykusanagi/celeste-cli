package loop

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
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
	return &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", SkipPersonaPrompt: true}
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
	prompt := env.SystemPrompt("CONTRACT-PROBE", nil)
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

func TestSetupChatPromptMatchesChatComposition(t *testing.T) {
	setupHome(t)
	env, _ := mustSetup(t, ModeChat, t.TempDir())
	if got, want := env.SystemPrompt("", nil), prompts.GetSystemPromptWithContext(true, env.ProjectContext, env.GitSnapshot); got != want {
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

func TestSetupBadSkillIsAWarning(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "skills", "broken.json"), "{not json")
	_, w := mustSetup(t, ModeAgent, t.TempDir())
	if !strings.Contains(w.all(), "custom skills") {
		t.Fatalf("warnings = %q, want the broken skill reported", w.all())
	}
}
