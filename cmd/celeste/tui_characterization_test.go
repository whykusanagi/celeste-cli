package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func chatApp(t *testing.T, srv *fakeprovider.Server) (tea.Model, *chatDeps, string) {
	t.Helper()
	return chatAppWithContextLimit(t, srv, 0)
}

// chatAppWithContextLimit is chatApp with an explicit config.ContextLimit
// override. Needed by TestTUISpillsHugeToolResult: "fake-model" is unknown,
// so config.ResolveContextLimit (cmd/celeste/config/tokens.go) guesses an
// 8.2K-token window, and (AppModel).compactContext (cmd/celeste/tui/compaction.go,
// via cmd/celeste/compact/compact.go) then elides ANY tool result that large
// relative to that tiny budget — replacing it with a
// "[... result elided to save context ...]" placeholder before the request
// this test inspects is even built. That happens whether or not
// ctxmgr.CapToolResult (cmd/celeste/context/limits.go) already capped the
// same result to a preview + spill notice; the elision pass runs afterward
// in (AppModel).buildToolFollowUpCmds and doesn't know the content was
// already a capped preview. A large override here isolates the CapToolResult
// spill mechanism this test targets from that separate pruning feature.
func chatAppWithContextLimit(t *testing.T, srv *fakeprovider.Server, contextLimit int) (tea.Model, *chatDeps, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: contextLimit}
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

type bigOutputTool struct{ builtin.BaseTool }

func (bigOutputTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{Content: strings.Repeat("x", 200*1024)}, nil
}

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
	var asked atomic.Int32
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		asked.Add(1)
		return tools.PermissionResponse{Decision: "deny"}
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write x.txt"}},
		func(m tea.Model) bool { return strings.Contains(lastAssistant(m), "denied") && turnIdle(m) }, 30*time.Second)
	if asked.Load() == 0 {
		t.Fatal("write_file did not ask for permission")
	}
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("denied write happened anyway")
	}
}

func TestTUISpillsHugeToolResult(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "big", Name: "big_output", Args: `{}`}}},
		fakeprovider.Turn{Text: "ok"},
	)
	m, deps, _ := chatAppWithContextLimit(t, srv, 1_000_000)
	deps.registry.Register(&bigOutputTool{BaseTool: builtin.BaseTool{
		ToolName:        "big_output",
		ToolDescription: "test: returns an oversized result",
		ToolParameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		ReadOnly:        true,
		ConcurrencySafe: true,
	}})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read big.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "ok" && turnIdle(m) }, 30*time.Second)
	var toolContents []string
	for _, x := range srv.Requests()[1].Body["messages"].([]any) {
		mm := x.(map[string]any)
		if mm["role"] == "tool" {
			c, _ := mm["content"].(string)
			toolContents = append(toolContents, c)
		}
	}
	if len(toolContents) != 1 {
		t.Fatalf("tool messages = %d, want 1", len(toolContents))
	}
	content := toolContents[0]
	if len(content) >= 128*1024 {
		t.Fatalf("tool result sent uncapped (%d bytes)", len(content))
	}

	// Today's actual pipeline has TWO independent, stacked truncation layers,
	// and the wire content only shows the outer one:
	//
	//  1. ctxmgr.CapToolResult (cmd/celeste/context/limits.go, invoked from
	//     ExecuteSkill in cmd/celeste/main.go right after the tool runs)
	//     spills the full 204800-byte result to disk and returns a
	//     131072-byte preview: [head]["...full output saved to: <path>..."][tail].
	//     This is the layer finding 2 originally targeted.
	//  2. (*llm.Client).trimHook / trimToolResults (cmd/celeste/llm/trim.go)
	//     runs later, once per outbound send, and re-trims ANY tool message
	//     over maxToolMsgBytes (64 KiB) down to that budget on a line
	//     boundary, appending its OWN "truncated ... for transport" notice.
	//     Layer 1's preview is 131072 bytes, i.e. already over the 64 KiB
	//     wire budget, so layer 2 fires on it too — and because layer 1's
	//     "full output saved to:" notice sits near the very end of its
	//     131072-byte preview (just before the 512-byte tail), layer 2's
	//     64 KiB head cut lands well before it. The text this test's first
	//     draft looked for never reaches the model; only layer 2's generic
	//     notice does. Verified empirically: content here is exactly 65536
	//     bytes and contains layer 2's notice, not layer 1's.
	//
	// So: assert layer 2's notice (what the model actually sees) and verify
	// layer 1's disk side effect (the full, uncapped result spilled to disk)
	// directly via its documented, deterministic path — sessionID
	// "tui-<pid>" (main.go's ExecuteSkill) and toolCallID "big" (this test's
	// fakeprovider.ToolCall.ID) — rather than by parsing it out of content
	// that no longer contains it.
	if !strings.Contains(content, "tool result truncated to ~65536 bytes for transport") {
		t.Fatalf("tool result missing the wire-trim notice: %q", content)
	}

	// The check above alone doesn't prove CapToolResult did any capping: if
	// CapToolResult returned the raw 204800-byte result unchanged (bug: it
	// still spills the file but skips building the preview), trimToolResults
	// would trim THAT down to 65536 bytes just the same, and the assertion
	// above would still pass — it only pins the outer (transport) trim, not
	// the inner (history) cap this test is meant to characterize. Pin what
	// trimToolResults says it received: trimToolResults' notice always
	// includes "original was %d bytes" for len(s) where s is whatever it was
	// handed (cmd/celeste/llm/trim.go, truncateWithNotice). If CapToolResult
	// is doing its job, that's its own 131072-byte capped preview, not the
	// raw 204800-byte tool output.
	if !strings.Contains(content, "original was 131072 bytes") {
		t.Fatalf("transport trim's reported input size != CapToolResult's 131072-byte cap; got: %q", content)
	}

	// Independently (and more directly) verify CapToolResult's own cap by
	// reading the chat history's tool message via DebugMessages — that's
	// what (AppModel) stored from ExecuteSkill's resultStr/capped value
	// (cmd/celeste/main.go), upstream of and unaffected by trimToolResults'
	// wire-only, copy-on-write pass (cmd/celeste/llm/trim.go doc comment:
	// "the caller's slice is never mutated"). It must be the capped preview
	// itself: exactly CapToolResult's maxBytes (131072) and containing its
	// spill notice, not the raw 204800-byte result.
	var historyContent string
	haveHistoryToolMsg := false
	for _, x := range chatMessages(m) {
		if x.Role == "tool" && x.ToolCallID == "big" {
			historyContent = x.Content
			haveHistoryToolMsg = true
		}
	}
	if !haveHistoryToolMsg {
		t.Fatal(`no tool-role message with ToolCallID "big" in chat history`)
	}
	if !strings.Contains(historyContent, "full output saved to:") {
		t.Fatalf("chat history tool result missing CapToolResult's own spill notice (len=%d): not capped", len(historyContent))
	}
	if len(historyContent) != 131072 {
		t.Fatalf("chat history tool result len = %d, want CapToolResult's capped preview (131072 bytes)", len(historyContent))
	}

	spillPath := filepath.Join(os.Getenv("HOME"), ".celeste", "tool-results",
		fmt.Sprintf("tui-%d", os.Getpid()), "big.txt")
	spilled, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("read spill file %q: %v", spillPath, err)
	}
	if got, want := len(spilled), 200*1024; got != want {
		t.Fatalf("spill file size = %d, want %d", got, want)
	}
	if string(spilled) != strings.Repeat("x", 200*1024) {
		t.Fatal("spill file does not contain the full tool output")
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
	d := newTUIDriver(t, m)
	d.Send(tui.SendMessageMsg{Content: "run sleep"})
	m = d.RunUntil(func(m tea.Model) bool {
		return len(srv.Requests()) == 1 && assistantHasToolCalls(m)
	}, 10*time.Second)
	d.Send(tui.SendMessageMsg{Content: "steer text"}, tea.KeyMsg{Type: tea.KeyEsc})

	// Today's actual behavior (verified empirically, not assumed — see the
	// comment below): (AppModel).interrupt (tui/turn_queue.go) cancels the
	// stream but leaves the executing bash tool and the queued steer alone.
	// Once the tool finishes, (AppModel).buildToolFollowUpCmds' very first
	// check is `if m.interrupted { ...; return m, nil }` ("Esc: the tools
	// have finished; don't ask the model to continue") — it returns WITHOUT
	// calling injectSteers(), so the steer is never folded into the aborted
	// turn. But AppModel.Update's wrapper unconditionally calls
	// dispatchQueued() (tui/turn_queue.go) after every update(), regardless
	// of message type; by the time buildToolFollowUpCmds returns,
	// turnActive() has gone false (toolBatchActive and toolProgress.Executing
	// both cleared, cancelFunc nil, typingContent empty since this turn was
	// tool-calls-only), so that same Update call's dispatchQueued() sees the
	// queue non-empty and fires a brand-new top-level SendMessage("steer
	// text") — not joined via injectSteers, just a fresh user turn.
	//
	// This means "idle AND queue drained" is briefly true in the *middle* of
	// that handoff, one Update() call before the new send actually starts
	// streaming again (confirmed with temporary t.Logf instrumentation: a
	// SkillResultMsg update leaves turnActive()==false && DebugQueued()==0
	// in the very same step that also returns the follow-up SendMessage
	// cmd). Waiting on that snapshot alone raced the driver into returning
	// before the dispatched cmd had run, abandoning it — the same class of
	// bug this driver rewrite exists to fix. Waiting for the new turn's
	// reply instead rides through that transient correctly.
	m = d.RunUntil(func(m tea.Model) bool {
		am := m.(tui.AppModel)
		return lastAssistant(am) == "after" && !am.DebugTurnActive() && am.DebugQueued() == 0
	}, 30*time.Second)

	requests := srv.Requests()
	if got := len(requests); got != 2 {
		t.Fatalf("requests after interrupt = %d, want 2", got)
	}
	if got := lastUserMessage(requests[1].Body["messages"].([]any)); got != "steer text" {
		t.Fatalf("second request last user message = %q, want steer text", got)
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
		if err == nil && len(sessions) > 0 && sessionHasToolAndFinalReply(sessions[0]) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no complete saved session after waiting for the async persistSession save: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	resumeSessionID = sessions[0].ID
	t.Cleanup(func() { resumeSessionID = "" })
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	tui.CloseLogging()
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

func assistantHasToolCalls(m tea.Model) bool {
	for _, msg := range chatMessages(m) {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func lastUserMessage(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i].(map[string]any)
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			return content
		}
	}
	return ""
}

func sessionHasToolAndFinalReply(session config.Session) bool {
	hasTool := false
	hasFinalReply := false
	for _, msg := range session.Messages {
		switch {
		case msg.Role == "tool":
			hasTool = true
		case msg.Role == "assistant" && msg.Content == "it says alpha":
			hasFinalReply = true
		}
	}
	return hasTool && hasFinalReply
}
