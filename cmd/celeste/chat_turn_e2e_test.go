package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Review Focus 4: "always allow" is saved to permissions.json and skips the
// ask for the next call, in the same turn and in a later one.
func TestTUIAlwaysAllowPersistsAndSkipsTheNextAsk(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w1", Name: "write_file", Args: `{"path":"x.txt","content":"x"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w2", Name: "write_file", Args: `{"path":"y.txt","content":"y"}`}}},
		fakeprovider.Turn{Text: "both written"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w3", Name: "write_file", Args: `{"path":"z.txt","content":"z"}`}}},
		fakeprovider.Turn{Text: "z written"},
	)
	m, deps, ws := chatApp(t, srv)
	var asked atomic.Int32
	deps.registry.SetPromptFunc(func(req tools.PermissionRequest) tools.PermissionResponse {
		asked.Add(1)
		return tools.PermissionResponse{Decision: "always_allow", Pattern: req.ToolName}
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write both"}},
		func(m tea.Model) bool { return lastAssistant(m) == "both written" && turnIdle(m) }, 30*time.Second)
	if n := asked.Load(); n != 1 {
		t.Fatalf("asked %d times, want 1", n)
	}
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write z"}},
		func(m tea.Model) bool { return lastAssistant(m) == "z written" && turnIdle(m) }, 30*time.Second)
	if n := asked.Load(); n != 1 {
		t.Fatalf("asked %d times after a later turn, want 1", n)
	}
	for _, f := range []string{"x.txt", "y.txt", "z.txt"} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Fatalf("%s not written: %v", f, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".celeste", "permissions.json"))
	if err != nil || !strings.Contains(string(b), "write_file") {
		t.Fatalf("permissions.json = %q, %v; want the write_file allow rule", b, err)
	}
}

// Review Focus 3: after a turn with a tool, a steer joined at the boundary
// and a streamed reply, the chat's LLM messages are exactly what the loop
// sent last, plus the final reply.
func TestChatHistoryMatchesLoopSnapshots(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "writing now", ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"x"}`}}},
		fakeprovider.Turn{Text: "done, and noted"},
	)
	m, deps, _ := chatApp(t, srv)
	release := make(chan struct{})
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		<-release
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	d := newTUIDriver(t, m)
	d.Send(tui.SendMessageMsg{Content: "write x.txt"})
	d.RunUntil(func(m tea.Model) bool { return assistantHasToolCalls(m) }, 30*time.Second)
	d.Send(tui.SendMessageMsg{Content: "also note it"})
	d.RunUntil(func(m tea.Model) bool { return m.(tui.AppModel).DebugQueued() == 1 }, 10*time.Second)
	close(release)
	m = d.RunUntil(func(m tea.Model) bool { return lastAssistant(m) == "done, and noted" && turnIdle(m) }, 30*time.Second)

	sent := requestMessages(t, srv, 1)
	var want []string
	for _, x := range sent {
		if x["role"] == "system" {
			continue
		}
		c, _ := x["content"].(string)
		want = append(want, x["role"].(string)+":"+c)
	}
	want = append(want, "assistant:done, and noted")
	var got []string
	for _, x := range m.(tui.AppModel).DebugLLMMessages() {
		got = append(got, x.Role+":"+x.Content)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("chat LLM history:\n%s\n\nwant (what the loop sent, plus the reply):\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A turn that prunes and then fails leaves the chat and the saved session
// with the pruned results the loop sent, not the full ones.
func TestTUIPrunedHistoryIsKeptWhenTheTurnFails(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Status: 400})
	m, _, _ := chatAppWithContextLimit(t, srv, 20_000)
	history := bigToolHistory()
	m = m.(tui.AppModel).WithMessages(history[:len(history)-1])
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "next"}},
		func(m tea.Model) bool { return len(srv.Requests()) == 1 && turnIdle(m) }, 30*time.Second)
	sent := map[string]string{}
	for _, x := range requestMessages(t, srv, 0) {
		if x["role"] == "tool" {
			sent[x["tool_call_id"].(string)], _ = x["content"].(string)
		}
	}
	if len(sent["r0"]) >= 40*1024 {
		t.Fatal("the loop did not prune r0")
	}
	for _, x := range m.(tui.AppModel).DebugLLMMessages() {
		if x.Role == "tool" && x.Content != sent[x.ToolCallID] {
			t.Fatalf("chat holds %s as %d bytes, the loop sent %d", x.ToolCallID, len(x.Content), len(sent[x.ToolCallID]))
		}
	}
	files, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".celeste", "sessions", "*.json"))
	if len(files) == 0 {
		t.Fatal("no session saved")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), strings.Repeat("x", 40*1024)) {
			t.Fatalf("the saved session still holds a full result the loop pruned")
		}
	}
}
