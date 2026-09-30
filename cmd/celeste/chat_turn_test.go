package main

import (
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
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
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
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("jev shadow never asked Jev: the Score path is not wired through the loop's compactor")
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
