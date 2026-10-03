package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

func stopReason(t *testing.T, res json.RawMessage) string {
	t.Helper()
	var out struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatal(err)
	}
	return out.StopReason
}

func TestPromptStreamsTextAndToolCalls(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "It says hi."})
	c := newTestClient(t, testConfig(srv, 0))
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hi"), 0o644)
	sid := c.newSession(ws)
	res, err := c.call("session/prompt", textPrompt(sid, "what does a.txt say?"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stopReason(t, res); got != "end_turn" {
		t.Fatalf("stopReason = %s", got)
	}
	calls := c.updatesOf("tool_call")
	if len(calls) != 1 || calls[0]["kind"] != "read" || calls[0]["title"] != "read_file: a.txt" || calls[0]["status"] != "in_progress" {
		t.Fatalf("tool_call = %+v", calls)
	}
	if calls[0]["toolCallId"] != "r1" || mustJSON(calls[0]["rawInput"]) != `{"path":"a.txt"}` {
		t.Fatalf("tool_call id/rawInput = %+v", calls[0])
	}
	if locs := mustJSON(calls[0]["locations"]); !strings.Contains(locs, "a.txt") {
		t.Fatalf("tool_call locations = %s", locs)
	}
	ups := c.updatesOf("tool_call_update")
	if len(ups) != 1 || ups[0]["status"] != "completed" || !strings.Contains(mustJSON(ups[0]["content"]), "hi") {
		t.Fatalf("tool_call_update = %+v", ups)
	}
	if got := c.agentText(); got != "It says hi." {
		t.Fatalf("agent text = %q", got)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Review Focus 5: the first prompt is held at its permission request.
func TestOnePromptAtATimePerSession(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w1", Name: "write_file", Args: `{"path":"x.txt","content":"x"}`}}},
		fakeprovider.Turn{Text: "done"})
	c := newTestClient(t, testConfig(srv, 0))
	release := make(chan struct{})
	asked := make(chan struct{}, 1)
	c.permit = func(map[string]any) map[string]any {
		asked <- struct{}{}
		<-release
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow_once"}}
	}
	sid := c.newSession(t.TempDir())
	first := c.callAsync("session/prompt", textPrompt(sid, "write x.txt"))
	select {
	case <-asked:
	case <-time.After(30 * time.Second):
		t.Fatal("the first prompt never asked for permission")
	}
	if _, err := c.call("session/prompt", textPrompt(sid, "again")); err == nil || err.Code != CodeBusy {
		t.Fatalf("second prompt error = %v, want %d", err, CodeBusy)
	}
	close(release)
	r := <-first
	if r.err != nil || stopReason(t, r.result) != "end_turn" {
		t.Fatalf("first prompt = %+v", r)
	}
}

func TestCapStopsWithMaxTurnRequests(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "list_files", Args: `{}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r2", Name: "list_files", Args: `{"path":"."}`}}})
	c := newTestClient(t, testConfig(srv, 1))
	sid := c.newSession(t.TempDir())
	res, err := c.call("session/prompt", textPrompt(sid, "list"))
	if err != nil || stopReason(t, res) != "max_turn_requests" {
		t.Fatalf("res = %s err = %v", res, err)
	}
}

func TestTodoResultSendsAPlan(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "t1", Name: "todo", Args: `{"action":"create","title":"write tests"}`}}},
		fakeprovider.Turn{Text: "ok"})
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	if _, err := c.call("session/prompt", textPrompt(sid, "plan it")); err != nil {
		t.Fatal(err)
	}
	plans := c.updatesOf("plan")
	if len(plans) != 1 {
		t.Fatalf("plan updates = %+v", plans)
	}
	entries := plans[0]["entries"].([]any)
	e := entries[0].(map[string]any)
	if len(entries) != 1 || e["content"] != "write tests" || e["status"] != "pending" || e["priority"] != "medium" {
		t.Fatalf("entries = %+v", entries)
	}
}

// Ruling 10: a provider error is a JSON-RPC internal error with the
// provider's message, and the history up to it is kept.
func TestProviderErrorIsAnInternalError(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"model exploded"}}`},
		fakeprovider.Turn{Text: "fine now"})
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	_, err := c.call("session/prompt", textPrompt(sid, "first"))
	if err == nil || err.Code != CodeInternal || !strings.Contains(err.Message, "model exploded") {
		t.Fatalf("error = %v", err)
	}
	if res, err := c.call("session/prompt", textPrompt(sid, "second")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("next prompt = %s %v", res, err)
	}
	reqs := srv.Requests()
	if body := string(reqs[len(reqs)-1].Raw); !strings.Contains(body, "first") || !strings.Contains(body, "second") {
		t.Fatalf("the next request lost the history: %s", body)
	}
}

// A guard stop ends the turn with its notice as agent text (ruling 10).
func TestGuardStopSendsItsNotice(t *testing.T) {
	call := fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "l", Name: "list_files", Args: `{}`}}}
	srv := fakeprovider.NewOpenAI(t, call, call, call, call)
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	res, err := c.call("session/prompt", textPrompt(sid, "loop"))
	if err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("res = %s err = %v", res, err)
	}
	if !strings.Contains(c.agentText(), "identical tool call") {
		t.Fatalf("agent text lacks the guard's notice: %q", c.agentText())
	}
}

func TestPromptErrors(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	if _, err := c.call("session/prompt", textPrompt("no-such-session", "hi")); err == nil || err.Code != CodeInvalidParams {
		t.Fatalf("unknown session error = %v", err)
	}
	sid := c.newSession(t.TempDir())
	empty := map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "image", "data": "aGk=", "mimeType": "image/png"}}}
	if _, err := c.call("session/prompt", empty); err == nil || err.Code != CodeInvalidParams {
		t.Fatalf("empty prompt error = %v", err)
	}
}
