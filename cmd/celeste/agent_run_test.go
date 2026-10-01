package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

// runCLIAgent runs `celeste agent` on a goal against srv, with a temp HOME
// holding cfg (base_url and api_key filled in) and, when given, a global
// hooks.json of hooks. The run must complete: runAgentCommand exits the
// process otherwise.
func runCLIAgent(t *testing.T, srv *fakeprovider.Server, cfg map[string]any, goal string, hooks ...map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg["api_key"], cfg["base_url"] = "k", srv.BaseURL()
	write := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".celeste", name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", cfg)
	if len(hooks) > 0 {
		write("hooks.json", map[string]any{"hooks": hooks})
	}
	runAgentCommand([]string{"-goal", goal, "-planner=false", "-workspace", t.TempDir(),
		"-auto-approve", "-no-artifacts", "-no-checkpoint", "-verbose=false"})
}

// `celeste agent`'s goal passes UserPromptSubmit once, before any model
// call (2.0 F2e); the hook's context reaches the model with it.
func TestCLIAgentGoalCarriesUserPromptSubmitContext(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	runCLIAgent(t, srv, map[string]any{"model": "m"}, "say done",
		map[string]any{"event": "UserPromptSubmit", "command": hooktest.Command(t, "context", "CLI-CTX")})
	msgs, _ := srv.Requests()[0].Body["messages"].([]any)
	var users []string
	for _, m := range msgs {
		if mm, _ := m.(map[string]any); mm["role"] == "user" {
			s, _ := mm["content"].(string)
			users = append(users, s)
		}
	}
	if len(users) != 1 || users[0] != "say done\n\n<hook-context>\nCLI-CTX\n</hook-context>" {
		t.Fatalf("user messages = %q, want the goal with its hook context", users)
	}
}

// Plain `celeste agent` runs on agent_model when one is set, as subagents
// and MCP agent mode do (2.0 F2e); without one, on model.
func TestCLIAgentUsesAgentModel(t *testing.T) {
	for _, tc := range []struct {
		cfg  map[string]any
		want string
	}{
		{map[string]any{"model": "chat-model", "agent_model": "agent-model"}, "agent-model"},
		{map[string]any{"model": "chat-model"}, "chat-model"},
	} {
		srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
		runCLIAgent(t, srv, tc.cfg, "say done")
		if got := srv.Requests()[0].Body["model"]; got != tc.want {
			t.Errorf("config %v: request model = %v, want %s", tc.cfg, got, tc.want)
		}
	}
}
