package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// drainOrchestrator follows the /orch event stream to its terminal event.
func drainOrchestrator(t *testing.T, cmd tea.Cmd) []tui.OrchestratorEventMsg {
	t.Helper()
	var out []tui.OrchestratorEventMsg
	deadline := time.After(90 * time.Second)
	for cmd != nil {
		got := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { got <- c() }(cmd)
		select {
		case m := <-got:
			ev, ok := m.(tui.OrchestratorEventMsg)
			if !ok {
				return out // channel closed
			}
			out = append(out, ev)
			cmd = ev.ReadNext()
		case <-deadline:
			t.Fatal("the /orch run did not finish")
		}
	}
	return out
}

// /orch lanes ask the chat's permission modal instead of silently denying
// (F2c; spec §3.2), and the run's goroutine finishes after the terminal
// event instead of blocking on a send nobody reads (C2). tui.LogInfo is a
// no-op here: InitLogging is never called.
func TestOrchestratorCommandAsksThroughTheTUIPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	t.Chdir(ws)
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Write hi to out.txt"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"},
	)
	var mu sync.Mutex
	var asked []string
	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10},
		promptFn: func(req tools.PermissionRequest) tools.PermissionResponse {
			mu.Lock()
			asked = append(asked, req.ToolName)
			mu.Unlock()
			return tools.PermissionResponse{Decision: "allow_once"}
		},
	}
	first := adapter.RunOrchestratorCommand("write hi to out.txt")
	events := drainOrchestrator(t, first)

	closed := make(chan tea.Msg, 1)
	go func() { closed <- first() }()
	select {
	case m := <-closed:
		if m != nil {
			t.Fatalf("an event after the terminal one: %#v", m)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the /orch goroutine did not finish after the terminal event")
	}
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err != nil {
		t.Fatalf("out.txt not written: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "write_file" {
		t.Fatalf("TUI prompt asked for %v, want [write_file]", asked)
	}
	for _, e := range events {
		if strings.Contains(e.Text, "no approval prompt") {
			t.Fatalf("/orch printed the headless notice although it has a prompt: %q", e.Text)
		}
	}
}
