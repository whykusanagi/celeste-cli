package acp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func loadParams(sid, cwd string) map[string]any {
	return map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}}
}

// Review Focus 4: an editor reopened on yesterday's thread sees it again,
// tool calls included, and the next prompt continues with the whole
// history (ruling 11).
func TestLoadReplaysAndContinues(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "It says load-marker."},
		fakeprovider.Turn{Text: "Still here."})
	home := t.TempDir()
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("load-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := newTestClientIn(t, testConfig(srv, 0), home)
	sid := first.newSession(ws)
	if res, err := first.call("session/prompt", textPrompt(sid, "what does a.txt say?")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("first prompt = %s %v", res, err)
	}

	// The editor restarts: a fresh agent over the same home.
	c := newTestClientIn(t, testConfig(srv, 0), home)
	if _, err := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	res, err := c.call("session/load", loadParams(sid, ws))
	if err != nil {
		t.Fatal(err)
	}
	if string(res) != "{}" {
		t.Fatalf("session/load = %s, want {}", res)
	}
	if got, want := c.updateKinds(), []string{UpdateUserMessageChunk, UpdateToolCall, UpdateAgentMessageChunk}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed updates = %v, want %v", got, want)
	}
	user := c.updatesOf(UpdateUserMessageChunk)[0]["content"].(map[string]any)
	if user["text"] != "what does a.txt say?" {
		t.Fatalf("user chunk = %+v", user)
	}
	call := c.updatesOf(UpdateToolCall)[0]
	if call["toolCallId"] != "r1" || call["status"] != "completed" || call["kind"] != "read" ||
		call["title"] != "read_file: a.txt" || !strings.Contains(mustJSON(call["content"]), "load-marker") {
		t.Fatalf("replayed tool_call = %+v", call)
	}
	if got := c.agentText(); got != "It says load-marker." {
		t.Fatalf("replayed agent text = %q", got)
	}

	if res, err := c.call("session/prompt", textPrompt(sid, "and now?")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("prompt after load = %s %v", res, err)
	}
	reqs := srv.Requests()
	body := string(reqs[len(reqs)-1].Raw)
	for _, want := range []string{"what does a.txt say?", `"r1"`, "load-marker", "It says load-marker.", "and now?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the request after load lacks %q:\n%s", want, body)
		}
	}
	// The continued thread is saved under the same session.
	data, rerr := os.ReadFile(filepath.Join(home, ".celeste", "sessions", sid+".json"))
	if rerr != nil || !strings.Contains(string(data), "Still here.") {
		t.Fatalf("saved session after load = %s (%v)", data, rerr)
	}
}

// A failed tool call replays as failed.
func TestLoadReplaysAFailedToolCall(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"missing.txt"}`}}},
		fakeprovider.Turn{Text: "No such file."})
	home := t.TempDir()
	ws := t.TempDir()
	first := newTestClientIn(t, testConfig(srv, 0), home)
	sid := first.newSession(ws)
	if _, err := first.call("session/prompt", textPrompt(sid, "read missing.txt")); err != nil {
		t.Fatal(err)
	}
	c := newTestClientIn(t, testConfig(srv, 0), home)
	if _, err := c.call("session/load", loadParams(sid, ws)); err != nil {
		t.Fatal(err)
	}
	calls := c.updatesOf(UpdateToolCall)
	if len(calls) != 1 || calls[0]["status"] != "failed" {
		t.Fatalf("replayed tool_call = %+v", calls)
	}
}

func TestLoadUnknownSession(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	ws := t.TempDir()
	for _, sid := range []string{"no-such-session", "../escape", ""} {
		if _, err := c.call("session/load", loadParams(sid, ws)); err == nil || err.Code != CodeInvalidParams {
			t.Fatalf("load %q: error = %v, want %d", sid, err, CodeInvalidParams)
		}
	}
	if _, err := c.call("session/load", loadParams("x", "relative")); err == nil || err.Code != CodeInvalidParams {
		t.Fatalf("relative cwd: error = %v", err)
	}
}

// The session record carries the editor's folder as its Workspace (W4d-1),
// so `celeste resume` lists editor threads with their project.
func TestSessionWorkspaceIsRecorded(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	ws := t.TempDir()
	sid := c.newSession(ws)
	got, err := config.NewSessionManager().Load(sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetWorkspace() != filepath.Clean(ws) {
		t.Fatalf("workspace = %q, want %q", got.GetWorkspace(), ws)
	}
}

// Loading a session this agent already has open replays its history
// without building a second Env.
func TestLoadOpenSessionReplays(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "open-reply"})
	c := newTestClient(t, testConfig(srv, 0))
	ws := t.TempDir()
	sid := c.newSession(ws)
	if _, err := c.call("session/prompt", textPrompt(sid, "hello")); err != nil {
		t.Fatal(err)
	}
	before := c.agent.session(sid)
	n := len(c.updatesOf(UpdateAgentMessageChunk))
	if _, err := c.call("session/load", loadParams(sid, ws)); err != nil {
		t.Fatal(err)
	}
	if c.agent.session(sid) != before {
		t.Fatal("loading an open session replaced it")
	}
	if got := len(c.updatesOf(UpdateAgentMessageChunk)); got != n+1 || len(c.updatesOf(UpdateUserMessageChunk)) != 1 {
		t.Fatalf("replay of an open session: %v", c.updateKinds())
	}
}
