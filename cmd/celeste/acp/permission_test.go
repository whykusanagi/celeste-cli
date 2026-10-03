package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func writeCall(id, path string) fakeprovider.Turn {
	return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "write_file", Args: `{"path":"` + path + `","content":"x"}`}}}
}

func selected(option string) map[string]any {
	return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": option}}
}

func TestPermissionAllowOnceAndAlways(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, writeCall("w1", "a.txt"), writeCall("w2", "b.txt"), writeCall("w3", "c.txt"), fakeprovider.Turn{Text: "done"})
	c := newTestClient(t, testConfig(srv, 0))
	var mu sync.Mutex
	var asks []map[string]any
	answers := []string{OptionAllowOnce, OptionAllowAlways}
	c.permit = func(p map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		asks = append(asks, p)
		// The editor has seen the tool call before it is asked about it.
		tc := p["toolCall"].(map[string]any)
		seen := false
		for _, u := range c.updatesOf("tool_call") {
			seen = seen || u["toolCallId"] == tc["toolCallId"]
		}
		if !seen {
			t.Errorf("permission asked for %v before its tool_call update", tc["toolCallId"])
		}
		if len(answers) == 0 {
			t.Errorf("asked again after allow_always: %+v", p["toolCall"])
			return selected(OptionRejectOnce)
		}
		answer := answers[0]
		answers = answers[1:]
		return selected(answer)
	}
	ws := t.TempDir()
	sid := c.newSession(ws)
	res, err := c.call("session/prompt", textPrompt(sid, "write three files"))
	if err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("prompt = %s %v", res, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asks) != 2 {
		t.Fatalf("asks = %d, want 2 (allow_always stops later asks for write_file)", len(asks))
	}
	tc := asks[0]["toolCall"].(map[string]any)
	if tc["toolCallId"] != "w1" || tc["kind"] != "edit" || tc["status"] != "pending" || tc["title"] != "write_file: a.txt" {
		t.Fatalf("toolCall = %+v", tc)
	}
	var kinds []string
	for _, o := range asks[0]["options"].([]any) {
		om := o.(map[string]any)
		kinds = append(kinds, om["kind"].(string)+"="+om["name"].(string))
	}
	if got := strings.Join(kinds, ","); got != "allow_once=Allow,allow_always=Always allow write_file,reject_once=Reject" {
		t.Fatalf("options = %s", got)
	}
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Fatalf("%s not written: %v", f, err)
		}
	}
	// "Always" is the session's, never written to permissions.json.
	if _, err := os.Stat(filepath.Join(c.home, ".celeste", "permissions.json")); !os.IsNotExist(err) {
		t.Fatalf("allow_always must not persist: %v", err)
	}
}

func TestPermissionRejectDenies(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, writeCall("w1", "a.txt"), fakeprovider.Turn{Text: "ok"})
	c := newTestClient(t, testConfig(srv, 0))
	c.permit = func(map[string]any) map[string]any { return selected(OptionRejectOnce) }
	ws := t.TempDir()
	sid := c.newSession(ws)
	if _, err := c.call("session/prompt", textPrompt(sid, "write")); err != nil {
		t.Fatal(err)
	}
	ups := c.updatesOf("tool_call_update")
	if len(ups) != 1 || ups[0]["status"] != "failed" || !strings.Contains(strings.ToLower(mustJSON(ups[0]["content"])), "denied") {
		t.Fatalf("tool_call_update = %+v", ups)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a rejected write ran: %v", err)
	}
}

// Review Focus 1: session/cancel while the editor shows a permission prompt.
func TestCancelDuringPermissionPrompt(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, writeCall("w1", "a.txt"), fakeprovider.Turn{Text: "after"})
	c := newTestClient(t, testConfig(srv, 0))
	asked := make(chan struct{}, 1)
	c.permit = func(map[string]any) map[string]any {
		asked <- struct{}{}
		return nil // the editor never answers
	}
	ws := t.TempDir()
	sid := c.newSession(ws)
	first := c.callAsync("session/prompt", textPrompt(sid, "write"))
	select {
	case <-asked:
	case <-time.After(30 * time.Second):
		t.Fatal("no permission request")
	}
	start := time.Now()
	c.notify("session/cancel", map[string]any{"sessionId": sid})
	select {
	case r := <-first:
		if r.err != nil || stopReason(t, r.result) != "cancelled" {
			t.Fatalf("cancelled prompt = %s %v", r.result, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt did not answer within 2s of session/cancel")
	}
	t.Logf("cancel answered after %s", time.Since(start))
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("the write ran after cancel: %v", err)
	}
	res, err := c.call("session/prompt", textPrompt(sid, "next"))
	if err != nil || stopReason(t, res) != "end_turn" || !strings.Contains(c.agentText(), "after") {
		t.Fatalf("next prompt = %s %v (text %q)", res, err, c.agentText())
	}
}

// A cancel sent right after a prompt, before the prompt's goroutine got to
// run, still cancels it.
func TestCancelRightAfterPrompt(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 12; i++ {
		turns = append(turns, fakeprovider.Turn{Text: "reply"})
	}
	srv := fakeprovider.NewOpenAI(t, turns...)
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	for i := 0; i < 10; i++ {
		ch := c.callAsync("session/prompt", textPrompt(sid, "hi"))
		c.notify("session/cancel", map[string]any{"sessionId": sid})
		r := <-ch
		if r.err != nil || stopReason(t, r.result) != "cancelled" {
			t.Fatalf("try %d: prompt then cancel = %s %v", i, r.result, r.err)
		}
	}
	// A cancel with no prompt in flight does not cancel a later prompt.
	c.notify("session/cancel", map[string]any{"sessionId": sid})
	time.Sleep(50 * time.Millisecond)
	if res, err := c.call("session/prompt", textPrompt(sid, "go")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("prompt after a stray cancel = %s %v", res, err)
	}
}

// A PreToolUse hook's "ask" forces the editor's prompt even after the user
// chose "Always allow" for the tool: the hook's policy is never skipped.
func TestPermissionHookAskOverridesAlwaysAllow(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, writeCall("w1", "a.txt"), writeCall("w2", "b.txt"), fakeprovider.Turn{Text: "done"})
	c := newTestClient(t, testConfig(srv, 0))
	hooksJSON, jerr := json.Marshal(map[string]any{"hooks": []any{map[string]any{
		"event": "PreToolUse", "matcher": "write_file", "command": hooktest.Command(t, "ask", "check writes"),
	}}})
	if jerr != nil {
		t.Fatal(jerr)
	}
	if err := os.MkdirAll(filepath.Join(c.home, ".celeste"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.home, ".celeste", "hooks.json"), hooksJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	asks := 0
	c.permit = func(map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		asks++
		return selected(OptionAllowAlways)
	}
	ws := t.TempDir()
	sid := c.newSession(ws)
	res, err := c.call("session/prompt", textPrompt(sid, "write two files"))
	if err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("prompt = %s %v", res, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if asks != 2 {
		t.Fatalf("asks = %d, want 2 (a hook's ask is asked even after allow_always)", asks)
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Fatalf("%s not written: %v", f, err)
		}
	}
}

// An ask without a call ID gets a toolCallId of its own, so two such asks
// never collide in the editor.
func TestPermissionWithoutCallIDIsUnique(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	var mu sync.Mutex
	var ids []any
	c.permit = func(p map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, p["toolCall"].(map[string]any)["toolCallId"])
		return selected(OptionAllowOnce)
	}
	s := c.agent.session(c.newSession(t.TempDir()))
	g := s.gate(c.agent, newPromptState())
	for range 2 {
		if r := g.Ask(context.Background(), tools.PermissionRequest{ToolName: "bash"}); r.Decision != "allow_once" {
			t.Fatalf("decision = %q", r.Decision)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || ids[0] == ids[1] || ids[0] == "" {
		t.Fatalf("toolCallIds = %v, want two distinct", ids)
	}
}
