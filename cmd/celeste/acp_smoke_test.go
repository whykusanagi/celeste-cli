//go:build acp_smoke

package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// smokeClient drives a celeste acp process over its real stdin and stdout,
// as an editor does.
type smokeClient struct {
	t       *testing.T
	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex
	nextID  int
	waiting map[int]chan smokeReply
	updates []map[string]any
	perms   chan map[string]any // session/request_permission params, unanswered
	done    chan struct{}
	stopped sync.Once
}

type smokeReply struct {
	Result json.RawMessage
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
}

func startACP(t *testing.T, bin, home string) *smokeClient {
	t.Helper()
	cmd := exec.Command(bin, "acp")
	// A clean environment: the user's CELESTE_* settings and keys never
	// reach the smoke run.
	cmd.Env = []string{
		"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(), "TEMP=" + os.TempDir(), "TMP=" + os.TempDir(),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "CELESTE_NO_AUTO_UPGRADE=1",
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &smokeClient{t: t, cmd: cmd, in: in, waiting: map[int]chan smokeReply{}, perms: make(chan map[string]any, 4), done: make(chan struct{})}
	go c.read(bufio.NewReader(out))
	t.Cleanup(func() { c.stop() })
	return c
}

func (c *smokeClient) read(r *bufio.Reader) {
	defer close(c.done)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
			smokeReply
		}
		if json.Unmarshal(line, &m) != nil {
			c.t.Errorf("stdout carried a non-JSON line: %s", line)
			continue
		}
		switch m.Method {
		case "session/update":
			c.mu.Lock()
			c.updates = append(c.updates, m.Params["update"].(map[string]any))
			c.mu.Unlock()
		case "session/request_permission":
			c.perms <- m.Params // never answered: the test cancels instead
		case "":
			var id int
			_ = json.Unmarshal(m.ID, &id)
			c.mu.Lock()
			ch := c.waiting[id]
			delete(c.waiting, id)
			c.mu.Unlock()
			if ch != nil {
				ch <- m.smokeReply
			}
		}
	}
}

func (c *smokeClient) send(v any) {
	b, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		c.t.Errorf("writing to celeste acp: %v", err)
	}
}

func (c *smokeClient) callAsync(method string, params any) <-chan smokeReply {
	ch := make(chan smokeReply, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.waiting[id] = ch
	c.mu.Unlock()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (c *smokeClient) wait(method string, ch <-chan smokeReply) json.RawMessage {
	c.t.Helper()
	select {
	case r := <-ch:
		if r.Error != nil {
			c.t.Fatalf("%s: error %d %s", method, r.Error.Code, r.Error.Message)
		}
		return r.Result
	case <-time.After(60 * time.Second):
		c.t.Fatalf("%s: no answer", method)
		return nil
	}
}

func (c *smokeClient) call(method string, params any) json.RawMessage {
	c.t.Helper()
	return c.wait(method, c.callAsync(method, params))
}

func (c *smokeClient) text(kind string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, u := range c.updates {
		if u["sessionUpdate"] != kind {
			continue
		}
		if content, ok := u["content"].(map[string]any); ok {
			b.WriteString(content["text"].(string))
		}
	}
	return b.String()
}

// stop closes stdin (the editor went away) and waits for the process.
func (c *smokeClient) stop() {
	c.stopped.Do(c.stopOnce)
}

func (c *smokeClient) stopOnce() {
	_ = c.in.Close()
	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			c.t.Errorf("celeste acp exited with %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = c.cmd.Process.Kill()
		c.t.Error("celeste acp did not exit when stdin closed")
	}
}

func stopReasonOf(t *testing.T, res json.RawMessage) string {
	t.Helper()
	var out struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatalf("prompt answer %s: %v", res, err)
	}
	return out.StopReason
}

// TestACPSmoke builds celeste and drives `celeste acp` end to end over real
// pipes: initialize, session/new, a prompt, a cancel while a tool waits on
// the editor's permission, then a restart and session/load (W4f-3).
//
//	go test -tags acp_smoke ./cmd/celeste -run TestACPSmoke -count=1 -v
func TestACPSmoke(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "celeste")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building celeste: %v", err)
	}

	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "smoke-reply"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w1", Name: "write_file", Args: `{"path":"x.txt","content":"x"}`}}},
		fakeprovider.Turn{Text: "after-load"})
	home := t.TempDir()
	cfg, _ := json.Marshal(map[string]any{"api_key": "k", "base_url": srv.BaseURL(), "model": "fake-model", "timeout": 10})
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".celeste", "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()

	c := startACP(t, bin, home)
	var init struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}), &init); err != nil ||
		init.ProtocolVersion != 1 || !init.AgentCapabilities.LoadSession {
		t.Fatalf("initialize = %+v (%v)", init, err)
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(c.call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{}}), &created); err != nil || created.SessionID == "" {
		t.Fatalf("session/new: %+v %v", created, err)
	}
	sid := created.SessionID
	prompt := func(c *smokeClient, text string) <-chan smokeReply {
		return c.callAsync("session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}})
	}
	if got := stopReasonOf(t, c.wait("session/prompt", prompt(c, "hello smoke"))); got != "end_turn" {
		t.Fatalf("first prompt stopReason = %s", got)
	}
	if got := c.text("agent_message_chunk"); !strings.Contains(got, "smoke-reply") {
		t.Fatalf("agent text = %q", got)
	}

	// A write waits on the editor's permission; the editor cancels.
	pending := prompt(c, "write x.txt")
	select {
	case <-c.perms:
	case <-time.After(60 * time.Second):
		t.Fatal("no permission request")
	}
	c.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	if got := stopReasonOf(t, c.wait("session/prompt", pending)); got != "cancelled" {
		t.Fatalf("cancelled prompt stopReason = %s", got)
	}
	if _, err := os.Stat(filepath.Join(ws, "x.txt")); err == nil {
		t.Fatal("the cancelled write ran")
	}
	c.stop()
	<-c.done

	// The editor restarts and reopens the thread.
	c2 := startACP(t, bin, home)
	c2.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if res := c2.call("session/load", map[string]any{"sessionId": sid, "cwd": ws, "mcpServers": []any{}}); string(res) != "{}" {
		t.Fatalf("session/load = %s", res)
	}
	if got := c2.text("user_message_chunk"); !strings.Contains(got, "hello smoke") {
		t.Fatalf("replayed user text = %q", got)
	}
	if got := c2.text("agent_message_chunk"); !strings.Contains(got, "smoke-reply") {
		t.Fatalf("replayed agent text = %q", got)
	}
	if got := stopReasonOf(t, c2.wait("session/prompt", prompt(c2, "still there?"))); got != "end_turn" {
		t.Fatalf("prompt after load stopReason = %s", got)
	}
	reqs := srv.Requests()
	if last := string(reqs[len(reqs)-1].Raw); !strings.Contains(last, "hello smoke") || !strings.Contains(last, "smoke-reply") {
		t.Fatalf("the prompt after load lacks the earlier turn:\n%s", last)
	}
}
