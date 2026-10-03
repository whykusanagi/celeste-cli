package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

type rpcReply struct {
	result json.RawMessage
	err    *RPCError
}

// testClient plays the editor: it talks to an Agent over in-memory pipes,
// records session/update notifications and answers permission requests.
type testClient struct {
	t       *testing.T
	agent   *Agent
	home    string
	in      io.Writer
	mu      sync.Mutex
	nextID  int
	waiting map[int]chan rpcReply
	updates []map[string]any
	// permit answers session/request_permission; nil selects "allow_once",
	// and a permit returning nil never answers.
	permit func(params map[string]any) map[string]any
	// logs are the agent's log lines.
	logMu sync.Mutex
	logs  []string
}

// testConfig points celeste at srv; a nil srv is for tests that never send
// a prompt (any request then fails to connect).
func testConfig(srv *fakeprovider.Server, maxIter int) func() (*config.Config, error) {
	base := "http://127.0.0.1:1/v1"
	if srv != nil {
		base = srv.BaseURL()
	}
	return func() (*config.Config, error) {
		return &config.Config{APIKey: "k", BaseURL: base, Model: "fake-model", Timeout: 10, MaxToolIterations: maxIter}, nil
	}
}

func newTestClient(t *testing.T, cfg func() (*config.Config, error)) *testClient {
	t.Helper()
	return newTestClientIn(t, cfg, t.TempDir())
}

// newTestClientIn starts an agent and its client with home as HOME: a
// second agent in the same home sees the first one's sessions.
func newTestClientIn(t *testing.T, cfg func() (*config.Config, error), home string) *testClient {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &testClient{t: t, home: home, in: inW, waiting: map[int]chan rpcReply{}}
	logf := func(format string, args ...any) {
		t.Logf(format, args...)
		c.logMu.Lock()
		c.logs = append(c.logs, fmt.Sprintf(format, args...))
		c.logMu.Unlock()
	}
	agent := NewAgent(Deps{Config: cfg, Sessions: config.NewSessionManager(), Home: home, Logf: logf})
	c.agent = agent
	conn := NewConn(inR, outW, agent)
	agent.Attach(conn)
	ctx, cancel := context.WithCancel(context.Background())
	go conn.Serve(ctx)
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close(); agent.Close() })
	go c.read(bufio.NewReader(outR))
	return c
}

// logged reports whether a log line contains sub.
func (c *testClient) logged(sub string) bool {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	for _, l := range c.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func (c *testClient) send(v any) {
	b, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.in.Write(append(b, '\n'))
}

func (c *testClient) read(r *bufio.Reader) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		if json.Unmarshal(line, &m) != nil {
			c.t.Errorf("stdout carried a non-JSON line: %s", line)
			continue
		}
		switch {
		case m.Method == "session/update":
			c.mu.Lock()
			c.updates = append(c.updates, m.Params["update"].(map[string]any))
			c.mu.Unlock()
		case m.Method == "session/request_permission":
			// Answered off the read loop, as an editor does: a permit that
			// holds its answer must not stop the client reading.
			c.mu.Lock()
			permit := c.permit
			c.mu.Unlock()
			go func(id json.RawMessage, params map[string]any) {
				answer := map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow_once"}}
				if permit != nil {
					answer = permit(params)
				}
				if answer != nil { // nil: never answer
					c.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": answer})
				}
			}(m.ID, m.Params)
		case m.Method == "":
			var id int
			_ = json.Unmarshal(m.ID, &id)
			c.mu.Lock()
			ch := c.waiting[id]
			delete(c.waiting, id)
			c.mu.Unlock()
			if ch != nil {
				ch <- rpcReply{m.Result, m.Error}
			}
		}
	}
}

func (c *testClient) callAsync(method string, params any) <-chan rpcReply {
	ch := make(chan rpcReply, 1)
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.waiting[id] = ch
	c.mu.Unlock()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (c *testClient) call(method string, params any) (json.RawMessage, *RPCError) {
	c.t.Helper()
	select {
	case r := <-c.callAsync(method, params):
		return r.result, r.err
	case <-time.After(30 * time.Second):
		c.t.Fatalf("%s: no answer", method)
		return nil, nil
	}
}

func (c *testClient) notify(method string, params any) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// newSession runs initialize and session/new for ws and returns the id.
func (c *testClient) newSession(ws string) string {
	c.t.Helper()
	if _, err := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		c.t.Fatal(err)
	}
	res, err := c.call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{}})
	if err != nil {
		c.t.Fatal(err)
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(res, &out)
	return out.SessionID
}

// sessionIDOf reads session/new's sessionId.
func sessionIDOf(t *testing.T, res json.RawMessage) string {
	t.Helper()
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &out); err != nil || out.SessionID == "" {
		t.Fatalf("session/new = %s (%v)", res, err)
	}
	return out.SessionID
}

func textPrompt(sid, text string) map[string]any {
	return map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": text}}}
}

// updatesOf returns the recorded updates of one kind, in order.
func (c *testClient) updatesOf(kind string) []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, u := range c.updates {
		if u["sessionUpdate"] == kind {
			out = append(out, u)
		}
	}
	return out
}

// agentText joins the agent_message_chunk texts.
func (c *testClient) agentText() string {
	var b strings.Builder
	for _, u := range c.updatesOf("agent_message_chunk") {
		if content, ok := u["content"].(map[string]any); ok {
			b.WriteString(content["text"].(string))
		}
	}
	return b.String()
}

// updateKinds is the sessionUpdate of every recorded update, in order.
func (c *testClient) updateKinds() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.updates))
	for _, u := range c.updates {
		k, _ := u["sessionUpdate"].(string)
		out = append(out, k)
	}
	return out
}
