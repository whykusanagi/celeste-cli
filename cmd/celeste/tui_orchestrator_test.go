package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// startOrchestrator runs the command /orch returns: a batch of the run's
// StreamStartMsg and the read of its first event. It returns the run's
// cancel, the read command (called again, it reads the next message) and
// the first event.
func startOrchestrator(t *testing.T, cmd tea.Cmd) (context.CancelFunc, tea.Cmd, tui.OrchestratorEventMsg) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("/orch did not return a batch with its cancel")
	}
	type result struct {
		cmd tea.Cmd
		msg tea.Msg
	}
	got := make(chan result, len(batch))
	for _, c := range batch {
		go func(c tea.Cmd) { got <- result{c, c()} }(c)
	}
	var cancel context.CancelFunc
	var read tea.Cmd
	var first tui.OrchestratorEventMsg
	for range batch {
		select {
		case r := <-got:
			switch m := r.msg.(type) {
			case tui.StreamStartMsg:
				cancel = m.Cancel
			case tui.OrchestratorEventMsg:
				read, first = r.cmd, m
			}
		case <-time.After(90 * time.Second):
			t.Fatal("the /orch run did not start")
		}
	}
	if cancel == nil || read == nil {
		t.Fatalf("/orch batch: cancel set %v, first event read %v", cancel != nil, read != nil)
	}
	return cancel, read, first
}

// drainOrchestrator follows the /orch event stream to its terminal event and
// returns the events and the read command.
func drainOrchestrator(t *testing.T, cmd tea.Cmd) ([]tui.OrchestratorEventMsg, tea.Cmd) {
	t.Helper()
	_, read, first := startOrchestrator(t, cmd)
	out := []tui.OrchestratorEventMsg{first}
	deadline := time.After(90 * time.Second)
	next := first.ReadNext()
	for next != nil {
		got := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { got <- c() }(next)
		select {
		case m := <-got:
			ev, ok := m.(tui.OrchestratorEventMsg)
			if !ok {
				return out, read // channel closed
			}
			out = append(out, ev)
			next = ev.ReadNext()
		case <-deadline:
			t.Fatal("the /orch run did not finish")
		}
	}
	return out, read
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
	events, read := drainOrchestrator(t, adapter.RunOrchestratorCommand("write hi to out.txt", 1))

	closed := make(chan tea.Msg, 1)
	go func() { closed <- read() }()
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

// A failed debate is not a failed run: "debate skipped" is a notice, not the
// terminal EventError, so /orch keeps reading and reaches EventComplete with
// the primary's output (F2c ruling; before, /orch stopped at the notice and
// dropped the result). The reviewer lane points at a closed port, so it
// fails at once without retries.
func TestOrchestratorCommandKeepsGoingAfterASkippedDebate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Fix it"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: fixed"},
	)
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	cfg.Orchestrator = &config.OrchestratorConfig{Lanes: map[string]config.LaneConfig{
		"code": {Primary: "fake-model", Reviewer: "fake-model", ReviewerBaseURL: "http://127.0.0.1:1"},
	}}
	adapter := &TUIClientAdapter{baseConfig: cfg}
	events, _ := drainOrchestrator(t, adapter.RunOrchestratorCommand("fix the bug in main.go", 1))

	if len(events) == 0 {
		t.Fatal("no events")
	}
	skipped := false
	for _, e := range events[:len(events)-1] {
		if strings.Contains(e.Text, "debate skipped") {
			skipped = true
			if e.Ch == nil {
				t.Fatalf("the debate-skipped event is terminal: %#v", e)
			}
		}
	}
	if !skipped {
		t.Fatalf("no debate-skipped notice before the last event: %#v", events)
	}
	last := events[len(events)-1]
	if last.Kind != 7 || !strings.Contains(last.Text, "fixed") {
		t.Fatalf("last event = kind %d %q, want EventComplete with the primary's output", last.Kind, last.Text)
	}
}

// modalPrompt is main.go's promptFn against a test driver: it shows the
// TUI's permission modal and waits for the key the user presses.
func modalPrompt(d *tuiTestDriver) tools.PromptFunc {
	return func(req tools.PermissionRequest) tools.PermissionResponse {
		ch := make(chan tui.PermissionResponse, 1)
		d.external <- tui.PermissionRequestMsg{ToolName: req.ToolName, InputSummary: req.InputSummary, RiskLevel: req.RiskLevel, Response: ch}
		r := <-ch
		return tools.PermissionResponse{Decision: r.Decision, Pattern: r.Pattern}
	}
}

func chatHas(m tea.Model, s string) bool {
	for _, msg := range chatMessages(m) {
		if strings.Contains(msg.Content, s) {
			return true
		}
	}
	return false
}

// Esc and Ctrl+C while an /orch lane's permission modal is up answer "deny":
// the lane gets the denial and the run finishes. Before, the modal took
// only a/A/d/D, and the run could not be cancelled either.
func TestOrchestratorModalEscAndCtrlCDeny(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		t.Run(key.String(), func(t *testing.T) {
			srv := writeScriptTUI(t)
			m, deps, ws := chatApp(t, srv)
			t.Chdir(ws)
			d := newTUIDriver(t, m)
			deps.adapter.promptFn = modalPrompt(d)
			d.Send(tui.SendMessageMsg{Content: "/orch write hi to out.txt"})
			d.RunUntil(func(m tea.Model) bool { return m.(tui.AppModel).DebugPermissionPromptActive() }, 30*time.Second)
			d.Send(key)
			d.RunUntil(func(m tea.Model) bool {
				return !m.(tui.AppModel).DebugPermissionPromptActive() && turnIdle(m) && !chatHas(m, "❌")
			}, 30*time.Second)
			if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
				t.Fatal("out.txt was written after the modal was dismissed")
			}
			var bodies []string
			for _, r := range srv.Requests() {
				bodies = append(bodies, fmt.Sprint(r.Body["messages"]))
			}
			if !strings.Contains(strings.Join(bodies, "\n"), "user denied execution of") {
				t.Fatalf("the lane never got the denial; %d requests:\n%s", len(bodies), strings.Join(bodies, "\n"))
			}
		})
	}
}

// writeScriptTUI is one /orch lane that writes out.txt.
func writeScriptTUI(t *testing.T) *fakeprovider.Server {
	return fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Write hi to out.txt"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: done"},
	)
}

// The TUI's interrupt (Esc on an empty input) cancels a running /orch: its
// lanes run on a context the TUI holds, not context.Background().
func TestOrchestratorInterruptCancelsTheRun(t *testing.T) {
	started, stop := make(chan struct{}, 8), make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select { // answers only when the client gives up
		case <-r.Context().Done():
			return
		case <-stop:
		}
	}))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(stop) })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	t.Chdir(ws)
	app, deps, err := newChatApp(&config.Config{APIKey: "k", BaseURL: hang.URL + "/v1", Model: "fake-model", Timeout: 120}, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	d := newTUIDriver(t, m)
	d.Send(tui.SendMessageMsg{Content: "/orch say hi"})
	go func() {
		<-started
		d.external <- tea.KeyMsg{Type: tea.KeyEsc}
	}()
	d.RunUntil(func(m tea.Model) bool { return m.(tui.AppModel).DebugInterrupted() }, 30*time.Second)
	// The server never answers, so the run's event stream ending (no
	// pending reads) means the interrupt cancelled it; a StreamStartMsg
	// arriving after Esc cancels its run then. The run's own EventError is
	// ignored, since Esc already ended the turn.
	m = d.RunUntil(func(tea.Model) bool { return d.pending == 0 }, 20*time.Second)
	if !turnIdle(m) || chatHas(m, "❌") {
		t.Fatalf("the cancelled run's error reached the chat or reopened the turn: %v", chatMessages(m))
	}
}

// After the terminal event /orch's callback drops events: it returns without
// sending (nobody reads) and without panicking once the channel is closed.
func TestOrchestratorEventSenderDropsEventsAfterTheTerminalOne(t *testing.T) {
	ch := make(chan tui.OrchestratorEventMsg, 1)
	send := orchestratorEventSender(ch, ch, 1)
	send(orchestrator.OrchestratorEvent{Kind: orchestrator.EventComplete, Text: "done"})
	if m := <-ch; m.Kind != int(orchestrator.EventComplete) || m.Ch != nil {
		t.Fatalf("terminal message = %#v, want EventComplete with no next read", m)
	}
	send(orchestrator.OrchestratorEvent{Kind: orchestrator.EventAction, Text: "late"})
	select {
	case m := <-ch:
		t.Fatalf("an event after the terminal one was sent: %#v", m)
	default:
	}
	close(ch)
	send(orchestrator.OrchestratorEvent{Kind: orchestrator.EventAction, Text: "after close"}) // must not panic
}

// An /orch lane's permission request names its run (2.0 F2e): the lanes run
// on a context tagged with the run the TUI numbered.
func TestOrchestratorRequestsNameTheirRun(t *testing.T) {
	srv := writeScriptTUI(t)
	_, deps, ws := chatApp(t, srv)
	t.Chdir(ws)
	got := make(chan tui.RunOwner, 1)
	deps.adapter.promptFn = func(req tools.PermissionRequest) tools.PermissionResponse {
		select {
		case got <- tui.RunOwnerFrom(req.Context):
		default:
		}
		return tools.PermissionResponse{Decision: "deny"}
	}
	for range collectCmd(deps.adapter.RunOrchestratorCommand("write hi to out.txt", 9)) {
	}
	select {
	case o := <-got:
		if o != (tui.RunOwner{Kind: tui.OwnerOrch, Run: 9}) {
			t.Fatalf("lane request owner = %+v", o)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the lane never asked for permission")
	}
}

// collectCmd runs cmd and follows every OrchestratorEventMsg's Ch to the end.
func collectCmd(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch m := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, m...)
		case tui.OrchestratorEventMsg:
			out = append(out, m)
			if m.Ch != nil {
				ch := m.Ch
				queue = append(queue, func() tea.Msg {
					next, ok := <-ch
					if !ok {
						return nil
					}
					return next
				})
			}
		default:
			out = append(out, msg)
		}
	}
	return out
}
