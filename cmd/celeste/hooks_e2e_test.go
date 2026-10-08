package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

func hookDef(t *testing.T, ev hooks.Event, matcher string, args ...string) hooks.Definition {
	t.Helper()
	return hooks.Definition{Event: ev, Matcher: matcher, Command: hooktest.Command(t, args...)}
}

func writeHooksFile(t *testing.T, path string, defs ...hooks.Definition) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"hooks": defs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// chatAppWithHooks is chatApp with a setup step that writes hook files
// before newChatApp loads them. The large context limit keeps the unknown
// fake model's guessed 8K window from auto-pruning or auto-summarizing.
func chatAppWithHooks(t *testing.T, srv *fakeprovider.Server, setup func(home, ws string)) (tea.Model, *chatDeps, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	setup(home, ws)
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: 1_000_000}
	app, deps, err := newChatApp(cfg, ws, home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return m, deps, home, ws
}

func globalHooks(home string) string { return filepath.Join(home, ".celeste", "hooks.json") }

func requestMessages(t *testing.T, srv *fakeprovider.Server, i int) []map[string]any {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) <= i {
		t.Fatalf("only %d requests, want request %d", len(reqs), i)
	}
	var out []map[string]any
	for _, m := range reqs[i].Body["messages"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}

func lastOfRole(msgs []map[string]any, role string) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i]["role"] == role {
			s, _ := msgs[i]["content"].(string)
			return s
		}
	}
	return ""
}

func hasSystemLine(m tea.Model, sub string) bool {
	for _, x := range chatMessages(m) {
		if x.Role == "system" && strings.Contains(x.Content, sub) {
			return true
		}
	}
	return false
}

func waitJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var payload map[string]any
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &payload) == nil {
			return payload
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never written", path)
	return nil
}

func longHistory(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: strings.Repeat("filler ", 15000)}},
		func(m tea.Model) bool { return lastAssistant(m) == "ack1" && turnIdle(m) }, 30*time.Second)
	return drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "continue"}},
		func(m tea.Model) bool { return lastAssistant(m) == "ack2" && turnIdle(m) }, 30*time.Second)
}

func TestTUIGlobalPreToolUseHookDeniesWrite(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"no"}`}}},
		fakeprovider.Turn{Text: "The hook stopped it."},
	)
	m, deps, _, ws := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreToolUse, "write_file", "deny", "policy says no"))
	})
	asked := 0
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		asked++
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "write x.txt"}},
		func(m tea.Model) bool { return strings.Contains(lastAssistant(m), "stopped") && turnIdle(m) }, 30*time.Second)
	if asked != 0 {
		t.Fatalf("permission prompt shown %d times for a hook-denied call", asked)
	}
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("hook-denied write happened")
	}
	if got := lastOfRole(requestMessages(t, srv, 1), "tool"); !strings.Contains(got, "Blocked by pre-tool hook: policy says no") {
		t.Fatalf("tool result sent to model = %q", got)
	}
}

func TestTUIPostToolUseHookContextReachesModel(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "read it"},
	)
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPostToolUse, "read_file", "context", "LINT-OK"))
		os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "read it" && turnIdle(m) }, 30*time.Second)
	got := lastOfRole(requestMessages(t, srv, 1), "tool")
	if !strings.HasPrefix(got, "<hook-context>\nLINT-OK") || !strings.Contains(got, "alpha") {
		t.Fatalf("tool result sent to model = %q", got)
	}
}

func TestTUIUntrustedRepoHooksAreSkipped(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "read it"},
	)
	m, deps, home, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hookDef(t, hooks.EventPreToolUse, "*", "deny", "repo says no"))
		os.WriteFile(filepath.Join(ws, ".grimoire"), []byte("# P\n\n## Hooks\n\n### PreToolUse\n- read_file: exit 1\n"), 0o644)
		os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	})
	if deps.hooks.Has(hooks.EventPreToolUse) {
		t.Fatal("untrusted repo hooks loaded without approval")
	}
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "read it" && turnIdle(m) }, 30*time.Second)
	if got := lastOfRole(requestMessages(t, srv, 1), "tool"); !strings.Contains(got, "alpha") || strings.Contains(got, "Blocked") {
		t.Fatalf("tool result = %q, want the file content and no hook block", got)
	}
	if _, err := os.Stat(hooks.TrustPath(home)); err == nil {
		t.Fatal("trusted.json written without a person approving")
	}
}

func TestTUIApprovedRepoHooksRun(t *testing.T) {
	calls := 0
	chatHookApprover = func(hooks.Source, hooks.TrustStatus) hooks.Answer { calls++; return hooks.AnswerYes }
	t.Cleanup(func() { chatHookApprover = nil })
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "blocked then"},
	)
	m, _, home, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hookDef(t, hooks.EventPreToolUse, "read_file", "deny", "repo says no"))
	})
	if calls != 1 {
		t.Fatalf("approver called %d times, want 1", calls)
	}
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "blocked then" && turnIdle(m) }, 30*time.Second)
	if got := lastOfRole(requestMessages(t, srv, 1), "tool"); !strings.Contains(got, "repo says no") {
		t.Fatalf("tool result = %q", got)
	}
	if _, err := os.Stat(hooks.TrustPath(home)); err != nil {
		t.Fatalf("approval not persisted: %v", err)
	}
}

// Review fix I1: a blocked prompt leaves the chat and the saved session, so
// no later request, compaction (which reads the chat) or resume (which
// reads the session) can send it.
func TestTUIUserPromptSubmitHookBlocksPrompt(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "next reply"})
	m, deps, home, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "deny", "no secrets"))
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "my password is hunter2"}},
		func(m tea.Model) bool {
			return hasSystemLine(m, "Prompt blocked by a UserPromptSubmit hook: no secrets") && turnIdle(m)
		}, 30*time.Second)
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("blocked prompt reached the provider (%d requests)", n)
	}
	for _, x := range chatMessages(m) {
		if x.Role == "user" && strings.Contains(x.Content, "hunter2") {
			t.Fatal("blocked prompt is still in the chat history")
		}
	}
	_ = filepath.WalkDir(filepath.Join(home, ".celeste", "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		var s config.Session
		if b, rerr := os.ReadFile(p); rerr == nil && json.Unmarshal(b, &s) == nil {
			for _, msg := range s.Messages {
				if strings.Contains(msg.Content, "hunter2") {
					t.Errorf("blocked prompt saved in session %s", filepath.Base(p))
				}
			}
		}
		return nil
	})

	deps.adapter.hooks = nil // stop blocking; the next prompt goes out
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "next"}},
		func(m tea.Model) bool { return lastAssistant(m) == "next reply" && turnIdle(m) }, 30*time.Second)
	for _, x := range requestMessages(t, srv, 0) {
		if strings.Contains(fmt.Sprint(x["content"]), "hunter2") {
			t.Fatal("a later request carried the blocked prompt")
		}
	}
}

func TestTUIUserPromptSubmitHookAddsContext(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "context", "PROMPT-CTX"))
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "first"}},
		func(m tea.Model) bool { return lastAssistant(m) == "one" && turnIdle(m) }, 30*time.Second)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "second"}},
		func(m tea.Model) bool { return lastAssistant(m) == "two" && turnIdle(m) }, 30*time.Second)
	count := 0
	for _, x := range requestMessages(t, srv, 1) {
		if x["role"] == "user" && strings.Contains(x["content"].(string), "PROMPT-CTX") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("user messages carrying hook context in request 2 = %d, want 2 (both prompts)", count)
	}
}

func TestTUISessionStartHookContextInSystemPrompt(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventSessionStart, "", "context", "SESSION-CTX"))
	})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hello"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hi" && turnIdle(m) }, 30*time.Second)
	if sys := lastOfRole(requestMessages(t, srv, 0), "system"); !strings.Contains(sys, "SESSION-CTX") {
		t.Fatal("SessionStart additionalContext missing from the system prompt")
	}
}

func TestTUIStopHookSeesFinalReply(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "all done"})
	var rec string
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		rec = filepath.Join(home, "stop.json")
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "record", rec))
	})
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "finish"}},
		func(m tea.Model) bool { return lastAssistant(m) == "all done" && turnIdle(m) }, 30*time.Second)
	if payload := waitJSON(t, rec); payload["last_message"] != "all done" {
		t.Fatalf("Stop payload = %v", payload)
	}
}

func TestTUIPreCompactHookBlocksSummary(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ack1"}, fakeprovider.Turn{Text: "ack2"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreCompact, "", "deny", "keep history"))
	})
	m = longHistory(t, m)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/compact"}},
		func(m tea.Model) bool {
			return hasSystemLine(m, "compaction blocked by a PreCompact hook: keep history")
		}, 30*time.Second)
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2 (no summarize call)", n)
	}
}

func TestTUIPreCompactHookSeesManualTrigger(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "ack1"}, fakeprovider.Turn{Text: "ack2"},
		fakeprovider.Turn{Text: "## Goal\nfiller\n## Next step\nnone"},
	)
	var rec string
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		rec = filepath.Join(home, "precompact.json")
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreCompact, "", "record", rec))
	})
	m = longHistory(t, m)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/compact"}},
		func(m tea.Model) bool { return hasSystemLine(m, "🗜 Context compacted:") }, 30*time.Second)
	if payload := waitJSON(t, rec); payload["trigger"] != "manual" {
		t.Fatalf("PreCompact payload = %v", payload)
	}
}

func TestTUIPreCompactNotFiredWhenNothingToSummarize(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hello back"})
	var rec string
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		rec = filepath.Join(home, "precompact.json")
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreCompact, "", "record", rec))
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hello"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hello back" && turnIdle(m) }, 30*time.Second)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "/compact"}},
		func(m tea.Model) bool { return hasSystemLine(m, "Context summary not applied") }, 30*time.Second)
	if _, err := os.Stat(rec); err == nil {
		t.Fatal("PreCompact fired although there was nothing to summarize")
	}
}

// Review minor: non-gating hook failures are visible in the chat.
func TestTUIHookWarningShownInChat(t *testing.T) {
	var mu sync.Mutex
	var got []string
	notify := func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() }
	hookNotify.Store(&notify)
	t.Cleanup(func() { hookNotify.Store(nil) })
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "read it"},
	)
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPostToolUse, "read_file", "canned", "garbage"))
		os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "read it" && turnIdle(m) }, 30*time.Second)
	mu.Lock()
	joined := strings.Join(got, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "PostToolUse hook") || !strings.Contains(joined, "failed") {
		t.Fatalf("hook warnings delivered to the chat = %q", joined)
	}
	drive(t, m, []tea.Msg{tui.HookWarningMsg{Text: "hooks: test warning"}},
		func(m tea.Model) bool { return hasSystemLine(m, "hooks: test warning") }, 5*time.Second)
}

func adapterWithHooks(t *testing.T, defs ...hooks.Definition) *TUIClientAdapter {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeHooksFile(t, globalHooks(home), defs...)
	runner, err := hooks.Load(hooks.Options{Workspace: t.TempDir(), Home: home, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	return &TUIClientAdapter{
		hooks:      runner,
		baseConfig: &config.Config{},
		summarize: func(context.Context, string, string) (string, error) {
			return "## Goal\nfiller\n## Next step\nnone", nil
		},
	}
}

func TestAdapterSummarizeFiresCompactHooks(t *testing.T) {
	dir := t.TempDir()
	pre, post := filepath.Join(dir, "pre.json"), filepath.Join(dir, "post.json")
	a := adapterWithHooks(t,
		hookDef(t, hooks.EventPreCompact, "", "record", pre),
		hookDef(t, hooks.EventPostCompact, "", "record", post))
	msgs := []tui.ChatMessage{
		{Role: "user", Content: strings.Repeat("filler ", 15000)},
		{Role: "assistant", Content: "ack1"},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "ack2"},
	}
	if _, err := a.SummarizeContext(context.Background(), msgs, ""); err != nil {
		t.Fatal(err)
	}
	if p := waitJSON(t, pre); p["trigger"] != "auto" {
		t.Fatalf("PreCompact payload = %v", p)
	}
	if p := waitJSON(t, post); p["trigger"] != "auto" || !strings.Contains(fmt.Sprint(p["summary"]), "filler") {
		t.Fatalf("PostCompact payload = %v", p)
	}
}

func TestHookLoadWarningSystemMessageHelper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	warning := hooks.DisabledWarning(fmt.Errorf("discover failed"))
	app := tui.NewApp(nil).WithSystemMessage(warning)
	if !hasSystemLine(app, "hooks disabled: discover failed") {
		t.Fatalf("warning system message missing")
	}
}

// Fix round 1: warnings raised while hooks load (here: an untrusted repo
// file skipped without an approver) are shown in the chat, not only on the
// stderr the alt screen hides.
func TestTUIHookLoadWarningsShownInChat(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "read it"},
	)
	var rec string
	m, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		rec = filepath.Join(home, "ran.json")
		writeHooksFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hookDef(t, hooks.EventPreToolUse, "*", "record", rec))
		os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	})
	if !hasSystemLine(m, "skipping") {
		t.Fatalf("no load warning in the chat: %v", chatMessages(m))
	}
	if deps.hooks.Has(hooks.EventPreToolUse) {
		t.Fatal("untrusted repo hook loaded without approval")
	}
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "read a.txt"}},
		func(m tea.Model) bool { return lastAssistant(m) == "read it" && turnIdle(m) }, 30*time.Second)
	if _, err := os.Stat(rec); err == nil {
		t.Fatal("untrusted repo hook ran")
	}
}

// Fix round 1: a UserPromptSubmit hook cut short by an interrupt is not a
// block (loop.PromptCheck returns an error, F2d): the chat marks it
// Cancelled and the TUI keeps the prompt without reporting a block.
func TestPromptHookInterruptKeepsPrompt(t *testing.T) {
	a := adapterWithHooks(t, hookDef(t, hooks.EventUserPromptSubmit, "", "allow"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg, verdict, err := a.checkPrompt(ctx, tui.ChatMessage{Role: "user", Content: "keep me"})
	if err == nil || verdict.Blocked || msg.Content != "keep me" {
		t.Fatalf("checkPrompt = %+v, %+v, %v; want an interrupt error, not a verdict", msg, verdict, err)
	}
	blocked := tui.PromptBlockedMsg{Cancelled: true, Content: "keep me"}

	srv := fakeprovider.NewOpenAI(t)
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {})
	app := m.(tui.AppModel).WithMessages([]tui.ChatMessage{{Role: "user", Content: "keep me"}})
	m, _ = app.Update(blocked)
	if hasSystemLine(m, "Prompt blocked") {
		t.Fatal("an interrupted hook was reported as a block")
	}
	kept := false
	for _, x := range chatMessages(m) {
		kept = kept || (x.Role == "user" && x.Content == "keep me")
	}
	if !kept {
		t.Fatal("interrupted prompt was dropped from the chat")
	}
}

// Final review fix 1: every unchecked user message is checked, not only the
// newest. Two steers join a turn as separate messages; the first is blocked,
// so it never reaches the provider and leaves the chat, while the second is
// sent with the tool results.
func TestTUIPromptHookChecksEveryQueuedSteer(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	m, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "denyif", "STEER-A", "no A"))
	})
	release := make(chan struct{})
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		<-release // hold the tool until both steers are queued
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	d := newTUIDriver(t, m)
	d.Send(tui.SendMessageMsg{Content: "write x.txt"})
	d.RunUntil(func(m tea.Model) bool {
		return len(srv.Requests()) == 1 && assistantHasToolCalls(m)
	}, 30*time.Second)
	d.Send(tui.SendMessageMsg{Content: "STEER-A first"}, tui.SendMessageMsg{Content: "steer B second"})
	d.RunUntil(func(m tea.Model) bool { return m.(tui.AppModel).DebugQueued() == 2 }, 10*time.Second)
	close(release)
	m = d.RunUntil(func(m tea.Model) bool { return lastAssistant(m) == "done" && turnIdle(m) }, 30*time.Second)

	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	sawB := false
	for _, x := range requestMessages(t, srv, 1) {
		c := fmt.Sprint(x["content"])
		if strings.Contains(c, "STEER-A") {
			t.Fatalf("blocked steer reached the provider: %q", c)
		}
		sawB = sawB || (x["role"] == "user" && strings.Contains(c, "steer B second"))
	}
	if !sawB {
		t.Fatal("the allowed steer was not sent")
	}
	for _, x := range chatMessages(m) {
		if x.Role == "user" && strings.Contains(x.Content, "STEER-A") {
			t.Fatal("blocked steer is still in the chat")
		}
	}
	if !hasSystemLine(m, "Prompt blocked by a UserPromptSubmit hook: no A") {
		t.Fatal("block not reported")
	}
}

// Final review fix 1: a prompt kept after an interrupt cut its hook short
// is checked before the next send, and dropped if blocked.
func TestTUIPromptKeptAfterInterruptIsChecked(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "reply"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "denyif", "KEPT-PROMPT", "not that"))
	})
	ts := time.Now()
	app := m.(tui.AppModel).WithMessages([]tui.ChatMessage{{Role: "user", Content: "KEPT-PROMPT", Timestamp: ts}})
	m, _ = app.Update(tui.PromptBlockedMsg{Cancelled: true, Content: "KEPT-PROMPT", Timestamp: ts})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "next"}},
		func(m tea.Model) bool { return lastAssistant(m) == "reply" && turnIdle(m) }, 30*time.Second)
	msgs := requestMessages(t, srv, 0)
	for _, x := range msgs {
		if strings.Contains(fmt.Sprint(x["content"]), "KEPT-PROMPT") {
			t.Fatal("the kept prompt was sent without its hook check")
		}
	}
	if got := lastOfRole(msgs, "user"); got != "next" {
		t.Fatalf("last user message sent = %q, want next", got)
	}
	for _, x := range chatMessages(m) {
		if x.Role == "user" && x.Content == "KEPT-PROMPT" {
			t.Fatal("blocked kept prompt is still in the chat")
		}
	}
}

// Fix round 2: after Esc interrupts a tool loop the history ends with tool
// results. A blocked next prompt must not send that history, which would
// resume the interrupted turn.
func TestTUIBlockedPromptAfterInterruptedToolLoopSendsNothing(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"x.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "resumed the interrupted turn"},
	)
	m, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventUserPromptSubmit, "", "denyif", "BLOCKME", "nope"))
	})
	release := make(chan struct{})
	deps.registry.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		<-release // hold the tool so Esc lands mid tool loop
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	d := newTUIDriver(t, m)
	d.Send(tui.SendMessageMsg{Content: "write x.txt"})
	d.RunUntil(func(m tea.Model) bool {
		return len(srv.Requests()) == 1 && assistantHasToolCalls(m)
	}, 30*time.Second)
	d.Send(tea.KeyMsg{Type: tea.KeyEsc})
	d.RunUntil(func(m tea.Model) bool { return m.(tui.AppModel).DebugInterrupted() }, 5*time.Second)
	close(release)
	m = d.RunUntil(func(m tea.Model) bool {
		msgs := chatMessages(m)
		return turnIdle(m) && len(msgs) > 0 && func() bool {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role != "system" {
					return msgs[i].Role == "tool"
				}
			}
			return false
		}()
	}, 30*time.Second)

	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "BLOCKME please"}},
		func(m tea.Model) bool {
			return hasSystemLine(m, "Prompt blocked by a UserPromptSubmit hook: nope") && turnIdle(m)
		}, 30*time.Second)
	time.Sleep(200 * time.Millisecond) // a wrongly sent request would land by now
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("requests = %d, want 1 (the blocked prompt resumed the interrupted turn)", n)
	}
	for _, x := range chatMessages(m) {
		if x.Role == "user" && strings.Contains(x.Content, "BLOCKME") {
			t.Fatal("blocked prompt is still in the chat")
		}
		if x.Role == "assistant" && strings.Contains(x.Content, "resumed") {
			t.Fatal("the interrupted turn was resumed")
		}
	}
}

// Intentional change (F2d Task 13): a Stop hook's deny continues the chat
// once, with its reason as a hidden instruction; a second deny is reported
// and ignored.
func TestTUIStopHookDenyContinuesOnce(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first"}, fakeprovider.Turn{Text: "second"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "deny", "KEEP-GOING"))
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "finish"}},
		func(m tea.Model) bool { return lastAssistant(m) == "second" && turnIdle(m) }, 30*time.Second)
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	if got := lastOfRole(requestMessages(t, srv, 1), "user"); got != "KEEP-GOING" {
		t.Fatalf("continuation sent as %q", got)
	}
	if !hasSystemLine(m, "A Stop hook asked to continue: KEEP-GOING") {
		t.Fatal("the continuation is not shown")
	}
	if !hasSystemLine(m, "one continuation per turn") {
		t.Fatal("the second deny was not reported")
	}
	for _, x := range chatMessages(m) {
		if x.Role == "user" && x.Content == "KEEP-GOING" {
			if hidden, _ := x.Metadata["hidden"].(bool); !hidden {
				t.Fatal("the continuation is shown as the user's own prompt")
			}
		}
	}
}

// A Stop hook's deny continues only while turns remain: with the turn cap
// spent, the deny is reported and the turn ends.
func TestTUIStopHookDenyWithoutTurnsLeftEndsTheTurn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first"}, fakeprovider.Turn{Text: "second"})
	m, deps, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventStop, "", "deny", "KEEP-GOING"))
	})
	deps.adapter.baseConfig.MaxToolIterations = 1 // no turn is running yet
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "finish"}},
		func(m tea.Model) bool { return lastAssistant(m) == "first" && turnIdle(m) }, 30*time.Second)
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
	if !hasSystemLine(m, "the turn has no turns left") {
		t.Fatal("the ignored deny was not reported")
	}
	if hasSystemLine(m, "A Stop hook asked to continue") {
		t.Fatal("the chat continued without turns left")
	}
}

// A deny without a reason continues with "Continue.", and the continuation
// starts from the history without the empty reply the Stop hook saw. The
// continuation is the hook's instruction, not a prompt: UserPromptSubmit
// does not check it.
func TestTUIStopHookDenyAfterAnEmptyReplyContinues(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: ""}, fakeprovider.Turn{Text: "second"})
	m, _, _, _ := chatAppWithHooks(t, srv, func(home, ws string) {
		writeHooksFile(t, globalHooks(home),
			hookDef(t, hooks.EventStop, "", "deny"),
			hookDef(t, hooks.EventUserPromptSubmit, "", "denyif", "Continue.", "not a prompt"))
	})
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "finish"}},
		func(m tea.Model) bool { return lastAssistant(m) == "second" && turnIdle(m) }, 30*time.Second)
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	var roles []string
	for _, x := range requestMessages(t, srv, 1) {
		if x["role"] == "system" {
			continue
		}
		c, _ := x["content"].(string)
		roles = append(roles, fmt.Sprintf("%v:%s", x["role"], c))
	}
	if want := []string{"user:finish", "user:Continue."}; fmt.Sprint(roles) != fmt.Sprint(want) {
		t.Fatalf("continuation request = %q, want %q", roles, want)
	}
	if hasSystemLine(m, "not a prompt") {
		t.Fatal("UserPromptSubmit checked the Stop hook's continuation")
	}
}
