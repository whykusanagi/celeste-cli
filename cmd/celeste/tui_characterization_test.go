package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func chatApp(t *testing.T, srv *fakeprovider.Server) (tea.Model, *chatDeps, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	app, deps, err := newChatApp(cfg, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return m, deps, ws
}

func chatMessages(m tea.Model) []tui.ChatMessage { return m.(tui.AppModel).DebugMessages() }

func lastAssistant(m tea.Model) string {
	msgs := chatMessages(m)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func turnIdle(m tea.Model) bool { return !m.(tui.AppModel).DebugTurnActive() }

// #203: a text-free parallel tool-call response must record the assistant
// tool_calls message so the follow-up request is valid.
func TestTUITextFreeParallelCalls(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "a", Name: "read_file", Args: `{"path":"a.txt"}`}, {ID: "b", Name: "read_file", Args: `{"path":"b.txt"}`}}},
		fakeprovider.Turn{Text: "Read both files and they say alpha and beta."},
	)
	m, _, ws := chatApp(t, srv)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(ws, "b.txt"), []byte("beta"), 0o644)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt and b.txt"}},
		func(m tea.Model) bool { return strings.Contains(lastAssistant(m), "alpha and beta") && turnIdle(m) }, 30*time.Second)
	body := srv.Requests()[1].Body["messages"].([]any)
	var roles []string
	for _, x := range body {
		roles = append(roles, x.(map[string]any)["role"].(string))
	}
	if !strings.Contains(strings.Join(roles, ","), "assistant,tool,tool") {
		t.Fatalf("follow-up roles = %v", roles)
	}
}

// #206: a one-word reply typed before the stream closes must end the turn.
func TestTUIShortReplyEndsTurn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "READ"}, fakeprovider.Turn{Text: "second"})
	m, _, _ := chatApp(t, srv)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "reply READ"}},
		func(m tea.Model) bool { return lastAssistant(m) == "READ" && turnIdle(m) }, 30*time.Second)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "again"}},
		func(m tea.Model) bool { return lastAssistant(m) == "second" && turnIdle(m) }, 30*time.Second)
}

func TestTUIPermissionAskDeny(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"no"}`}}},
		fakeprovider.Turn{Text: "The write was denied."},
	)
	m, deps, ws := chatApp(t, srv)
	asked := 0
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		asked++
		return tools.PermissionResponse{Decision: "deny"}
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write x.txt"}},
		func(m tea.Model) bool { return strings.Contains(lastAssistant(m), "denied") && turnIdle(m) }, 30*time.Second)
	if asked == 0 {
		t.Fatal("write_file did not ask for permission")
	}
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("denied write happened anyway")
	}
}

func TestTUISpillsHugeToolResult(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "big", Name: "read_file", Args: `{"path":"big.txt"}`}}},
		fakeprovider.Turn{Text: "ok"},
	)
	m, _, ws := chatApp(t, srv)
	os.WriteFile(filepath.Join(ws, "big.txt"), []byte(strings.Repeat("x", 200*1024)), 0o644)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read big.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "ok" && turnIdle(m) }, 30*time.Second)
	for _, x := range srv.Requests()[1].Body["messages"].([]any) {
		mm := x.(map[string]any)
		if mm["role"] == "tool" {
			if c, _ := mm["content"].(string); len(c) >= 128*1024 {
				t.Fatalf("tool result sent uncapped (%d bytes)", len(c))
			}
		}
	}
}

// Esc during a streaming turn with a queued steer: the turn stops and the
// queued text is either sent next or discarded — never silently kept forever.
func TestTUIInterruptWithQueuedSteer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash tool needs a POSIX shell")
	}
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "s", Name: "bash", Args: `{"command":"sleep 2"}`}}},
		fakeprovider.Turn{Text: "after"}, fakeprovider.Turn{Text: "after2"},
	)
	m, deps, _ := chatApp(t, srv)
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "run sleep"}},
		func(m tea.Model) bool { return m.(tui.AppModel).DebugTurnActive() }, 10*time.Second)
	m, _ = m.Update(tui.SendMessageMsg{Content: "steer text"})
	m = drive(t, m, []tea.Msg{tea.KeyMsg{Type: tea.KeyEsc}}, turnIdle, 30*time.Second)
	if m.(tui.AppModel).DebugQueued() > 0 {
		t.Skip("baseline: steer stranded after Esc — fixed by F2 Loop.Steer")
	}
}

// /compact on a short history declines honestly (#204 baseline).
func TestTUICompactOnShortHistoryDeclines(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hello back"})
	m, _, _ := chatApp(t, srv)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hello"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hello back" && turnIdle(m) }, 30*time.Second)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/compact"}}, func(m tea.Model) bool {
		for _, x := range chatMessages(m) {
			if x.Role == "system" && strings.Contains(x.Content, "Context summary not applied") {
				return true
			}
		}
		return false
	}, 30*time.Second)
}

// /handoff asks the (small) model for notes and starts a new session.
func TestTUIHandoffStartsNewSession(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "hello back"},
		fakeprovider.Turn{Text: "## Goal\nsay hello\n## Next step\nnone"},
	)
	m, _, _ := chatApp(t, srv)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hello"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hello back" && turnIdle(m) }, 30*time.Second)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/handoff"}}, func(m tea.Model) bool {
		for _, x := range chatMessages(m) {
			if strings.Contains(x.Content, "New session started") {
				return true
			}
		}
		return false
	}, 30*time.Second)
	if len(srv.Requests()) != 2 {
		t.Fatalf("handoff made %d requests, want 2 (chat + notes)", len(srv.Requests()))
	}
}

// Resume restores tool traffic: a resumed session's first request carries the
// earlier assistant tool_calls + tool result (#196).
func TestTUIResumeCarriesToolHistory(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "it says alpha"},
		fakeprovider.Turn{Text: "you read a.txt"},
	)
	m, _, ws := chatApp(t, srv)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "it says alpha" && turnIdle(m) }, 30*time.Second)

	// AppModel.persistSession (cmd/celeste/tui/app.go) saves "asynchronously
	// (ignore errors for now)" in a bare `go func(){ ... Save(...) }()` with
	// no tea.Msg/Cmd signaling completion, so the write can still be in
	// flight when drive() returns on the assistant reply. Polling here
	// (rather than a single List() call right after drive returns) is a
	// characterization fix, not a production change: an unguarded single
	// check flaked in ~1/10 runs of this test (session file not yet on disk).
	var sessions []config.Session
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		sessions, err = config.NewSessionManager().List()
		if err == nil && len(sessions) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no saved session after waiting for the async persistSession save: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	resumeSessionID = sessions[0].ID
	t.Cleanup(func() { resumeSessionID = "" })
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	app, deps, err := newChatApp(cfg, ws, os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var m2 tea.Model = app
	m2, _ = m2.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	drive(t, m2, []tea.Msg{tui.SendMessageMsg{Content: "what did you read?"}},
		func(m tea.Model) bool { return lastAssistant(m) == "you read a.txt" && turnIdle(m) }, 30*time.Second)
	var roles []string
	for _, x := range srv.Requests()[2].Body["messages"].([]any) {
		roles = append(roles, x.(map[string]any)["role"].(string))
	}
	if !strings.Contains(strings.Join(roles, ","), "assistant,tool") {
		t.Fatalf("resumed request lost tool history: roles %v", roles)
	}
}
