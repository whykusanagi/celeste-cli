package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// chatResult is one MCP `celeste` mode:"chat" call's tool result.
type chatResult struct {
	Text    string
	IsError bool
}

// chatVia runs one mode:"chat" call through a fresh stdio server and checks
// the frozen response shape: exactly one text block.
func chatVia(t *testing.T, cfg Config, ws, prompt string) chatResult {
	t.Helper()
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": prompt, "mode": "chat", "workspace": ws}}})
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res[1], &parsed); err != nil || len(parsed.Content) != 1 || parsed.Content[0].Type != "text" {
		t.Fatalf("chat result is not one text block: %s", res[1])
	}
	return chatResult{Text: parsed.Content[0].Text, IsError: parsed.IsError}
}

// testHome is the temp HOME contractCfg set for this test.
func testHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// toolContent returns what the model was sent for tool call id in a
// recorded request, or "".
func toolContent(req fakeprovider.Request, id string) string {
	msgs, _ := req.Body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" && mm["tool_call_id"] == id {
			s, _ := mm["content"].(string)
			return s
		}
	}
	return ""
}

// Baseline: the 25-turn cap and its text. Each call writes a different file,
// so neither guard trips, and the 25th turn's call still runs.
func TestMCPChatTurnCapAt25(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 30; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{
			ID: fmt.Sprintf("w%d", i), Name: "write_file", Args: fmt.Sprintf(`{"path":"f%d.txt","content":"%d"}`, i, i),
		}}})
	}
	fp := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, fp)
	r := chatVia(t, cfg, ws, "write thirty files")
	if n := len(fp.Requests()); n != 25 {
		t.Fatalf("requests = %d, want 25", n)
	}
	if r.IsError || r.Text != "Tool loop limit reached" {
		t.Fatalf("got %+v, want the cap text", r)
	}
	if _, err := os.Stat(filepath.Join(ws, "f24.txt")); err != nil {
		t.Fatalf("the 25th turn's call did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "f25.txt")); !os.IsNotExist(err) {
		t.Fatalf("a 26th turn ran: %v", err)
	}
}

// Baseline: the guards' stop texts, whole and exact.
func TestMCPChatGuardTextsAreExact(t *testing.T) {
	cases := []struct {
		name string
		args func(i int) string
		want string
	}{
		{"identical", func(int) string { return `{"path":"main.go"}` },
			"Stopped: the model made the identical tool call 3 times in a row (stuck loop)."},
		{"progress", func(i int) string {
			if i%2 == 1 {
				return `{"path":"main.go","start_line":1}`
			}
			return `{"path":"main.go"}`
		}, "Stopped: the model called the same tool with no new result 6 turns in a row (stuck loop)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var turns []fakeprovider.Turn
			for i := 0; i < 12; i++ {
				turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: tc.args(i)}}})
			}
			fp := fakeprovider.NewOpenAI(t, turns...)
			cfg, ws := contractCfg(t, fp)
			if r := chatVia(t, cfg, ws, "loop"); r.IsError || r.Text != tc.want {
				t.Fatalf("got %+v, want %q", r, tc.want)
			}
		})
	}
}

// Baseline: spawn_agent never runs in MCP chat, so a claimed spawn is
// replaced.
func TestMCPChatStripsUnbackedSpawnClaim(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Subagent spawned (id: task-47), on it."})
	cfg, ws := contractCfg(t, fp)
	const want = "I described spawning a subagent, but no subagent was actually spawned this run (the spawn_agent tool did not run, so any agent id mentioned is not real). Please retry the spawn explicitly."
	if r := chatVia(t, cfg, ws, "spawn one"); r.IsError || r.Text != want {
		t.Fatalf("got %+v", r)
	}
}

// Baseline: the final reply is trimmed.
func TestMCPChatTrimsTheFinalReply(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "\n  All done.  \n"})
	cfg, ws := contractCfg(t, fp)
	if r := chatVia(t, cfg, ws, "hi"); r.IsError || r.Text != "All done." {
		t.Fatalf("got %+v", r)
	}
}

// Baseline: a provider failure is an isError result "Error: chat error: …".
func TestMCPChatProviderErrorIsAnErrorResult(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"fake bad request","type":"invalid_request_error"}}`})
	cfg, ws := contractCfg(t, fp)
	if r := chatVia(t, cfg, ws, "hi"); !r.IsError || !strings.HasPrefix(r.Text, "Error: chat error: ") {
		t.Fatalf("got %+v", r)
	}
}

// Baseline (#187/#190): Trust mode through the user's permission checker.
// The default always-deny rule stops sudo; ordinary writes need no approval.
func TestMCPChatDeniesSudoInTrustMode(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "b", Name: "bash", Args: `{"command":"sudo ls"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"note.txt","content":"hello\n"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	cfg, ws := contractCfg(t, fp)
	if r := chatVia(t, cfg, ws, "try sudo, then write"); r.IsError || r.Text != "done" {
		t.Fatalf("got %+v", r)
	}
	reqs := fp.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	if got := toolContent(reqs[1], "b"); !strings.Contains(got, "Permission denied") {
		t.Fatalf("sudo was not denied: %q", got)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "note.txt")); err != nil || string(b) != "hello\n" {
		t.Fatalf("trust mode did not allow write_file: %v %q", err, b)
	}
}

// Baseline (#187): the user's own always_deny rules hold in Trust mode.
func TestMCPChatHonoursUserDenyRules(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"note.txt","content":"x"}`}}},
		fakeprovider.Turn{Text: "done"},
	)
	cfg, ws := contractCfg(t, fp)
	writeFile(t, filepath.Join(testHome(t), ".celeste", "permissions.json"),
		`{"mode":"default","always_deny":[{"tool_pattern":"write_file","decision":"deny"}]}`)
	chatVia(t, cfg, ws, "write")
	if got := toolContent(fp.Requests()[1], "w"); !strings.Contains(got, "Permission denied") {
		t.Fatalf("user deny rule not applied: %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied write_file wrote anyway: %v", err)
	}
}
