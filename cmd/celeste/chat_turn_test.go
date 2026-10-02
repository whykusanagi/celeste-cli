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
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// runTurnMsgs runs one turn through the adapter, reading its events the
// way the program does, and returns every inner message up to TurnDoneMsg.
func runTurnMsgs(t *testing.T, a *TUIClientAdapter, req tui.TurnRequest) []tea.Msg {
	t.Helper()
	_, cmd := a.RunTurn(req)
	return drainTurn(t, cmd, req)
}

// drainTurn follows a turn's command chain to TurnDoneMsg.
func drainTurn(t *testing.T, cmd tea.Cmd, req tui.TurnRequest) []tea.Msg {
	t.Helper()
	return drainTurnWith(t, cmd, req, nil)
}

// drainTurnWith is drainTurn calling on (when set) with each message as it
// arrives, the way the app would see it.
func drainTurnWith(t *testing.T, cmd tea.Cmd, req tui.TurnRequest, on func(tea.Msg)) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	deadline := time.After(30 * time.Second)
	for cmd != nil {
		got := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { got <- c() }(cmd)
		select {
		case msg := <-got:
			ev, ok := msg.(tui.TurnEventMsg)
			if !ok {
				t.Fatalf("got %T, want tui.TurnEventMsg", msg)
			}
			if ev.Run != req.Run {
				t.Fatalf("event for run %d, want %d", ev.Run, req.Run)
			}
			out = append(out, ev.Msg)
			if on != nil {
				on(ev.Msg)
			}
			cmd = ev.Next
		case <-deadline:
			t.Fatalf("turn did not finish; got %d messages", len(out))
		}
	}
	return out
}

func userTurn(text string) []tui.ChatMessage {
	return []tui.ChatMessage{{Role: "user", Content: text, Timestamp: time.Now()}}
}

// kinds lists message types, without text chunks (their count depends on
// how the provider splits the text).
func turnKinds(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		if _, chunk := m.(tui.StreamChunkMsg); !chunk {
			out = append(out, fmt.Sprintf("%T", m))
		}
	}
	return out
}

func TestRunTurnDeliversTextThenDone(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi there"})
	_, deps, _ := chatApp(t, srv)
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hello"), Tools: true, Run: 7})
	// The first HistoryMsg is the checked prompt (EventPromptsChecked):
	// checkPrompt allows every prompt when no hook is configured.
	want := []string{"tui.HistoryMsg", "tui.TurnStartMsg", "tui.StreamDoneMsg", "tui.HistoryMsg", "tui.TurnDoneMsg"}
	if got := turnKinds(msgs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("messages = %v\nwant %v", got, want)
	}
	checked := msgs[0].(tui.HistoryMsg).History
	if done, _ := checked[0].Metadata[tui.MetaPromptHookDone].(bool); len(checked) != 1 || !done {
		t.Fatalf("first snapshot = %+v, want the prompt marked checked", checked)
	}
	if first, ok := msgs[2].(tui.StreamChunkMsg); !ok || !first.Chunk.IsFirst {
		t.Fatalf("third message = %#v, want the first text chunk", msgs[2])
	}
	if done := msgs[len(msgs)-1].(tui.TurnDoneMsg); done.Stop != "done" || done.Err != nil {
		t.Fatalf("done = %+v", done)
	}
}

func TestRunTurnToolEventsInOrder(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "it says alpha"},
	)
	_, deps, ws := chatApp(t, srv)
	writeFile(t, ws, "a.txt", "alpha")
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("read a.txt"), Tools: true, Run: 1})
	want := []string{
		"tui.HistoryMsg",
		"tui.TurnStartMsg", "tui.ToolTurnMsg", "tui.HistoryMsg", "tui.ToolStartMsg", "tui.ToolResultMsg", "tui.HistoryMsg",
		"tui.TurnStartMsg", "tui.StreamDoneMsg", "tui.HistoryMsg", "tui.TurnDoneMsg",
	}
	if got := turnKinds(msgs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("messages = %v\nwant %v", got, want)
	}
	recorded := msgs[3].(tui.HistoryMsg).History
	if last := recorded[len(recorded)-1]; last.Role != "assistant" || len(last.ToolCalls) != 1 {
		t.Fatalf("calls snapshot ends with %+v, want the tool_calls message", last)
	}
	if res := msgs[5].(tui.ToolResultMsg); !strings.Contains(res.Content, "alpha") || res.IsError {
		t.Fatalf("tool result = %+v", res)
	}
}

// Review Focus 2: an Enter that lands after the run took its leftovers is
// still in the loop's queue, and Leftover hands it back.
func TestTurnHandleLeftoverReturnsASteerTypedAfterTheRun(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	req := tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1}
	h, cmd := deps.adapter.RunTurn(req)
	msgs := drainTurn(t, cmd, req)
	if done := msgs[len(msgs)-1].(tui.TurnDoneMsg); len(done.Leftover) != 0 {
		t.Fatalf("done.Leftover = %v, want none", done.Leftover)
	}
	h.Steer("late") // TurnDoneMsg is out, not yet handled by the app
	if got := h.Leftover(); len(got) != 1 || got[0] != "late" {
		t.Fatalf("Leftover = %v, want [late]", got)
	}
	if got := h.Leftover(); len(got) != 0 {
		t.Fatalf("second Leftover = %v, want none", got)
	}
}

// The loop records an empty reply; the chat never did (it shows a system
// line), so no snapshot carries an empty text-only assistant message.
func TestRunTurnEmptyReplyLeavesNoAssistantMessage(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: ""})
	_, deps, _ := chatApp(t, srv)
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1})
	for _, m := range msgs {
		h, ok := m.(tui.HistoryMsg)
		if !ok {
			continue
		}
		for _, x := range h.History {
			if x.Role == "assistant" && x.Content == "" && len(x.ToolCalls) == 0 {
				t.Fatalf("snapshot carries an empty assistant message: %+v", h.History)
			}
		}
	}
}

// Review Focus 5: no tools offered means no tool definitions sent.
func TestRunTurnWithoutToolsSendsNoToolDefinitions(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: false, Run: 1})
	if defs, ok := srv.Requests()[0].Body["tools"].([]any); ok && len(defs) > 0 {
		t.Fatalf("sent %d tool definitions with tools off", len(defs))
	}
}

// A prompt the UserPromptSubmit hook blocks never reaches the provider.
func TestRunTurnStopsOnBlockedPrompt(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "should not happen"})
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "denyif", "BLOCKME", "nope"))
	})
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("BLOCKME now"), Tools: true, Run: 1})
	blocked, ok := msgs[0].(tui.PromptBlockedMsg)
	if !ok || blocked.Reason != "nope" || blocked.Content != "BLOCKME now" || blocked.Steer {
		t.Fatalf("first message = %#v", msgs[0])
	}
	if done := msgs[len(msgs)-1].(tui.TurnDoneMsg); done.Stop != "blocked" {
		t.Fatalf("done = %+v", done)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

// Review Focus 1: the program has quit (nobody reads events) while the
// permission modal waits for an answer that never comes. Cancelling the
// life context must end the run, and shutdown must return.
func TestChatTurnEndsAfterQuit(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"no"}`}}},
		fakeprovider.Turn{Text: "never"},
	)
	_, deps, _ := chatApp(t, srv)
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	asked := make(chan struct{})
	var once sync.Once
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		once.Do(func() { close(asked) })
		<-never
		return tools.PermissionResponse{Decision: "deny"}
	})
	_, cmd := deps.adapter.RunTurn(tui.TurnRequest{History: userTurn("write x.txt"), Tools: true, Run: 1})
	go cmd() // the program reads the first event, then quits
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("the permission prompt was never asked")
	}
	if !deps.adapter.shutdown(5 * time.Second) {
		t.Fatal("the turn did not end within 5s of quitting")
	}
}

// Jev shadow pruning (compact Options.Score) still runs, now inside the
// loop's compactor: the shadow report asks Jev after a prune.
func TestChatCompactorRunsJevShadow(t *testing.T) {
	var hits atomic.Int32
	jevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "test", http.StatusTeapot)
	}))
	defer jevSrv.Close()
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	a.baseConfig.JevPrune = "shadow"
	a.jev, a.jevFor = &jev.Client{Key: "k", URL: jevSrv.URL}, a.baseConfig

	big := strings.Repeat("x", 40*1024)
	history := userTurn("read the files")
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: big})
	}
	history = append(history, userTurn("next")...)
	msgs := runTurnMsgs(t, a, tui.TurnRequest{History: history, Tools: true, Window: 20_000, Run: 1})
	pruned := false
	for _, m := range msgs {
		_, ok := m.(tui.CompactedMsg)
		pruned = pruned || ok
	}
	if !pruned {
		t.Fatal("the loop's compactor pruned nothing (3 × 40 KiB results against a 20k-token window)")
	}
	// Generous: the shadow request runs in the background and -race slows it.
	deadline := time.Now().Add(30 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("jev shadow never asked Jev: the Score path is not wired through the loop's compactor")
	}
}

// The turn's compactor uses the Jev client resolved when RunTurn was called,
// on the Update goroutine; the run never reads the adapter's config, which
// an endpoint or profile switch replaces (Task 9 review ruling).
func TestRunTurnResolvesJevOnceForTheTurn(t *testing.T) {
	var hits atomic.Int32
	jevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "test", http.StatusTeapot)
	}))
	defer jevSrv.Close()
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	a.baseConfig.JevPrune = "shadow"
	a.jev, a.jevFor = &jev.Client{Key: "k", URL: jevSrv.URL}, a.baseConfig

	big := strings.Repeat("x", 40*1024)
	history := userTurn("read the files")
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: big})
	}
	history = append(history, userTurn("next")...)
	req := tui.TurnRequest{History: history, Tools: true, Window: 20_000, Run: 1}
	_, cmd := a.RunTurn(req)
	// A profile switch after the turn started: jev_prune is off in the new
	// config. The running turn keeps the client it started with.
	switched := *a.baseConfig
	switched.JevPrune = ""
	a.baseConfig = &switched
	drainTurn(t, cmd, req)
	// Generous: the shadow request runs in the background and -race slows it.
	deadline := time.Now().Add(30 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("the turn's compactor re-read the adapter's config instead of the Jev client resolved at RunTurn")
	}
}

func TestMailboxKeepsOrderAndNeverBlocks(t *testing.T) {
	b := newMailbox()
	for i := 0; i < 10000; i++ {
		b.put(i) // no reader: must not block
	}
	b.close()
	b.put("after close") // dropped
	for i := 0; i < 10000; i++ {
		if msg, ok := b.get(); !ok || msg != i {
			t.Fatalf("get %d = %v, %v", i, msg, ok)
		}
	}
	if msg, ok := b.get(); ok {
		t.Fatalf("get after drain = %v", msg)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An empty text-only reply already in the chat's history (a resumed
// session, a turn from before the loop) must not shift SyncLLM's
// positions: every snapshot drops it, so without care the chat would keep
// its copy of the next prompt and append the loop's too, and the duplicate
// would be checked by UserPromptSubmit again on the next send.
func TestRunTurnEmptyReplyInTheChatHistoryDoesNotDuplicateAPrompt(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	now := time.Now()
	chat := tui.NewChatModel()
	for _, m := range []tui.ChatMessage{
		{Role: "user", Content: "u1", Timestamp: now.Add(-2 * time.Second)},
		{Role: "assistant", Content: "", Timestamp: now.Add(-time.Second)},
		{Role: "user", Content: "u2", Timestamp: now},
	} {
		chat = chat.AppendLLM(m)
	}
	req := tui.TurnRequest{History: chat.GetLLMMessages(), Tools: true, Run: 1}
	msgs := runTurnMsgs(t, deps.adapter, req)
	snapshots := 0
	for _, m := range msgs {
		h, ok := m.(tui.HistoryMsg)
		if !ok {
			continue
		}
		snapshots++
		chat = chat.SyncLLM(h.History, false)
		var got []string
		for _, x := range chat.GetLLMMessages() {
			got = append(got, x.Role+":"+x.Content)
		}
		if n := strings.Count(strings.Join(got, "|"), "user:u2"); n != 1 {
			t.Fatalf("after snapshot %d the chat holds u2 %d times: %v", snapshots, n, got)
		}
		for _, x := range chat.GetLLMMessages() {
			if x.Role == "assistant" && x.Content == "" && len(x.ToolCalls) == 0 {
				t.Fatalf("after snapshot %d the chat keeps an empty reply: %v", snapshots, got)
			}
			if x.Role == "user" && x.Content == "u2" && !x.Timestamp.Equal(now) {
				t.Fatalf("u2's timestamp changed: %v, want %v", x.Timestamp, now)
			}
		}
	}
	if snapshots == 0 {
		t.Fatal("no HistoryMsg")
	}
	// The provider never sees the empty reply either (the OpenAI backend
	// also drops it, so this holds with or without the adapter's input
	// filter; the filter keeps the loop's own history aligned).
	sent, _ := srv.Requests()[0].Body["messages"].([]any)
	for _, raw := range sent {
		m, _ := raw.(map[string]any)
		if m["role"] == "assistant" && (m["content"] == nil || m["content"] == "") && m["tool_calls"] == nil {
			t.Fatalf("request carried an empty assistant message: %v", sent)
		}
	}
}

// readTurnLog runs fn with the log file on and returns what it logged.
func readTurnLog(t *testing.T, fn func()) string {
	t.Helper()
	if err := tui.InitLogging(); err != nil {
		t.Fatal(err)
	}
	path := tui.GetLogPath()
	fn()
	tui.CloseLogging()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The log file records each request (endpoint, model, message and tool
// counts), each response and its usage, and the session cost, as the chat
// did before it ran on the loop. Nothing of it reaches the chat.
func TestRunTurnLogsRequestsResponsesAndCost(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "it says alpha"},
	)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	writeFile(t, ws, "a.txt", "alpha")
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "gpt-4.1", Timeout: 10}
	_, deps, err := newChatApp(cfg, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var msgs []tea.Msg
	log := readTurnLog(t, func() {
		msgs = runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("read a.txt"), Tools: true, Run: 1})
	})
	for _, want := range []string{
		"→ Sending request to: " + srv.BaseURL() + " (model: gpt-4.1)",
		"LLM_REQUEST: 1 messages,",
		"LLM_RESPONSE: 0 chars, HAS TOOL CALLS",
		"LLM requested tool call: read_file",
		"LLM_REQUEST: 3 messages,",
		"LLM_RESPONSE: 13 chars, no tool calls",
		"Usage: 100 prompt + 10 completion = 110 tokens",
		"Session cost: $",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if n := strings.Count(log, "→ Sending request to:"); n != 2 {
		t.Errorf("logged %d requests, want 2", n)
	}
	if strings.Contains(log, "LLM_REQUEST: 1 messages, 0 tools") {
		t.Errorf("no tools counted:\n%s", log)
	}
	for _, m := range msgs {
		if w, ok := m.(tui.HookWarningMsg); ok {
			t.Errorf("the log reached the chat: %q", w.Text)
		}
	}
}

// A failed request logs the error with the endpoint and model.
func TestRunTurnLogsTheErrorWithEndpointAndModel(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Status: 400})
	_, deps, _ := chatApp(t, srv)
	var msgs []tea.Msg
	log := readTurnLog(t, func() {
		msgs = runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1})
	})
	if done := msgs[len(msgs)-1].(tui.TurnDoneMsg); done.Stop != "error" {
		t.Fatalf("done = %+v, want an error", done)
	}
	for _, want := range []string{"LLM error: ", "  Endpoint: " + srv.BaseURL(), "  Model: fake-model"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// The chat hears when the Stop hook starts, after the reply and before the
// turn ends, and only when a Stop hook is configured.
func TestRunTurnAnnouncesTheStopHook(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "done"})
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "allow"))
	})
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1})
	want := []string{"tui.HistoryMsg", "tui.TurnStartMsg", "tui.StreamDoneMsg", "tui.HistoryMsg", "tui.StopHookStartMsg", "tui.TurnDoneMsg"}
	if got := turnKinds(msgs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("messages = %v\nwant %v", got, want)
	}

	srv2 := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "done"})
	_, deps2, _ := chatApp(t, srv2)
	for _, m := range runTurnMsgs(t, deps2.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1}) {
		if _, ok := m.(tui.StopHookStartMsg); ok {
			t.Fatal("StopHookStartMsg without a Stop hook")
		}
	}
}

// warnings captures what reaches the chat through hookNotify.
func captureHookNotify(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	notify := func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() }
	hookNotify.Store(&notify)
	t.Cleanup(func() { hookNotify.Store(nil) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// boxMsgs closes a turn's mailbox and returns what was put in it.
func boxMsgs(b *mailbox) []tea.Msg {
	b.close()
	var out []tea.Msg
	for {
		msg, ok := b.get()
		if !ok {
			return out
		}
		out = append(out, msg)
	}
}

// Esc while the Stop hook runs ends the turn as interrupted: the hook is
// cut short, and that is not reported as a failed hook.
func TestRunTurnEscDuringTheStopHookEndsQuietly(t *testing.T) {
	warned := captureHookNotify(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "done"})
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "sleep"))
	})
	req := tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1}
	h, cmd := deps.adapter.RunTurn(req)
	start := time.Now()
	msgs := drainTurnWith(t, cmd, req, func(msg tea.Msg) {
		if _, ok := msg.(tui.StopHookStartMsg); ok {
			h.Cancel()
		}
	})
	if d := time.Since(start); d > 8*time.Second {
		t.Fatalf("the turn took %v: Esc did not cut the Stop hook short", d)
	}
	for _, m := range msgs {
		if w, ok := m.(tui.HookWarningMsg); ok {
			t.Errorf("warning in the turn: %q", w.Text)
		}
		if _, ok := m.(tui.StopContinueMsg); ok {
			t.Error("an interrupted turn continued")
		}
	}
	if got := warned(); len(got) != 0 {
		t.Errorf("warnings reached the chat: %q", got)
	}
}

// A turn already interrupted when its reply is in does not run the Stop
// hook (Stop never fires on an interrupt).
func TestStopHookIsSkippedAfterAnInterrupt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "stop.json")
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "record", marker))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	turn := &chatTurn{ctx: ctx, cancel: cancel, box: newMailbox()}
	if next := deps.adapter.stopHook(turn, "the reply", false, 5); next != "" {
		t.Fatalf("stopHook = %q, want no continuation", next)
	}
	if msgs := boxMsgs(turn.box); len(msgs) != 0 {
		t.Fatalf("messages = %v, want none", turnKinds(msgs))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the Stop hook ran after the interrupt")
	}
}

// escAfter reports an interrupt once path exists: Esc landing after the
// Stop hooks decided, before the chat acted on their answer.
type escAfter struct {
	context.Context
	path string
}

func (c escAfter) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		return context.Canceled
	}
	return nil
}

// A Stop hook's deny that races Esc does not continue the turn.
func TestStopHookDenyRacingEscDoesNotContinue(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "decided.json")
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home),
			hookDef(t, hooks.EventStop, "", "record", marker),
			hookDef(t, hooks.EventStop, "", "deny", "KEEP-GOING"))
	})
	turn := &chatTurn{ctx: escAfter{Context: context.Background(), path: marker}, cancel: func() {}, box: newMailbox()}
	if next := deps.adapter.stopHook(turn, "the reply", false, 5); next != "" {
		t.Fatalf("stopHook = %q after Esc, want no continuation", next)
	}
}

// A Stop hook that fails for its own reasons is still reported in the chat,
// before the turn ends.
func TestRunTurnReportsAFailedStopHook(t *testing.T) {
	warned := captureHookNotify(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "done"})
	_, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "exit", "1", "boom"))
	})
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1})
	var got []string
	for _, m := range msgs {
		if w, ok := m.(tui.HookWarningMsg); ok {
			got = append(got, w.Text)
		}
	}
	got = append(got, warned()...)
	if joined := strings.Join(got, "\n"); !strings.Contains(joined, "Stop hook") || !strings.Contains(joined, "boom") {
		t.Fatalf("warnings = %q, want the failed Stop hook", got)
	}
}

// The cap notice after a Stop continuation counts every model turn of the
// user's turn, not only the continuation's.
func TestRunTurnCapNoticeCountsTheWholeTurn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "first"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
	)
	_, deps, _, ws := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "deny", "KEEP-GOING"))
	})
	writeFile(t, ws, "a.txt", "alpha")
	deps.adapter.baseConfig.MaxToolIterations = 2
	msgs := runTurnMsgs(t, deps.adapter, tui.TurnRequest{History: userTurn("hi"), Tools: true, Run: 1})
	done := msgs[len(msgs)-1].(tui.TurnDoneMsg)
	if done.Stop != "cap" || !strings.Contains(done.Notice, "after 2 turn(s)") {
		t.Fatalf("done = %+v, want the cap after 2 turns", done)
	}
}

// bigToolHistory is a history whose old tool results a 20k-token window
// prunes.
func bigToolHistory() []tui.ChatMessage {
	big := strings.Repeat("x", 40*1024)
	history := userTurn("read the files")
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: big})
	}
	return append(history, userTurn("next")...)
}

// A turn that prunes and then ends before the model's reply (an error, an
// interrupt) still hands the chat the pruned history, and says how many
// tokens the prune saved.
func TestRunTurnSyncsTheCompactedHistoryWhenTheTurnEndsEarly(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop string
	}{{"error", "error"}, {"interrupt", "interrupted"}} {
		t.Run(tc.name, func(t *testing.T) {
			var baseURL string
			if tc.stop == "error" {
				baseURL = fakeprovider.NewOpenAI(t, fakeprovider.Turn{Status: 400}).BaseURL()
			} else {
				hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
				t.Cleanup(hang.Close)
				baseURL = hang.URL
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			cfg := &config.Config{APIKey: "k", BaseURL: baseURL, Model: "fake-model", Timeout: 10}
			_, deps, err := newChatApp(cfg, t.TempDir(), home)
			if err != nil {
				t.Fatal(err)
			}
			cleanupChatDeps(t, deps)
			history := bigToolHistory()
			req := tui.TurnRequest{History: history, Tools: true, Window: 20_000, Run: 1}
			h, cmd := deps.adapter.RunTurn(req)
			msgs := drainTurnWith(t, cmd, req, func(msg tea.Msg) {
				if _, ok := msg.(tui.CompactedMsg); ok && tc.stop == "interrupted" {
					h.Cancel()
				}
			})
			if done := msgs[len(msgs)-1].(tui.TurnDoneMsg); done.Stop != tc.stop {
				t.Fatalf("done = %+v, want %s", done, tc.stop)
			}
			compacted := false
			chat := tui.NewChatModel()
			for _, m := range history {
				chat = chat.AppendLLM(m)
			}
			for _, m := range msgs {
				switch m := m.(type) {
				case tui.CompactedMsg:
					compacted = true
					if m.Saved <= 0 {
						t.Errorf("CompactedMsg.Saved = %d, want the tokens the prune saved", m.Saved)
					}
				case tui.HistoryMsg:
					chat = chat.SyncLLM(m.History, false)
				}
			}
			if !compacted {
				t.Fatal("nothing was pruned")
			}
			for _, m := range chat.GetLLMMessages() {
				if m.Role == "tool" && m.ToolCallID == "r0" && len(m.Content) >= 40*1024 {
					t.Fatal("the chat still holds the full result the loop pruned")
				}
			}
		})
	}
}

func TestChatCompactorCountsSystemPromptAndTools(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	_, deps, _ := chatApp(t, srv)
	a := deps.adapter
	history := userTurn("read the files")
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 16_000)})
	}
	if est := compact.Estimate(history); est >= compact.Threshold(40_000) {
		t.Fatalf("test setup: history alone (%d) should be under the threshold", est)
	}
	c := &chatCompactor{a: a, window: 40_000, meter: compact.NewMeter(12_000)}
	_, notes, changed := c.Compact(context.Background(), history, nil, false)
	if !changed || len(notes) == 0 {
		t.Fatal("the compactor ignored the system prompt and tool schemas")
	}
}

// The chat's summaries keep KeepFor(window) of the newest history, so a
// 40k-window chat can be summarized once it is past ~10k (#234).
func TestChatSummaryKeepsByTheWindow(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nread the files"})
	_, deps, _ := chatAppWithContextLimit(t, srv, 40_000)
	// ~16k tokens: inside the old fixed 20k tail, over a 40k window's 10k.
	history := userTurn("read the files")
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 16_000)})
	}
	history = append(history, userTurn("next")...)
	out, err := deps.adapter.SummarizeContext(context.Background(), history, "")
	if err != nil {
		t.Fatalf("SummarizeContext: %v (a 40k window keeps 10k, so this history shrinks)", err)
	}
	if out.Cut == 0 {
		t.Fatal("nothing was summarized")
	}
}

// #200 in the chat: /compact's summary carries the workspace's todos.
func TestChatSummaryCarriesAuthoritativeState(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "## Goal\nread the files"})
	_, deps, ws := chatApp(t, srv)
	builtin.NewTodoStore(ws).Create("port the lexer", "")
	out, err := deps.adapter.SummarizeContext(context.Background(), bigToolHistory(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Messages[0].Content, "port the lexer (pending)") {
		t.Fatalf("summary lacks the todo list:\n%s", out.Messages[0].Content)
	}
}
