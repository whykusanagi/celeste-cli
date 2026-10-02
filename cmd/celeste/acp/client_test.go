package acp

import (
	"bufio"
	"context"
	"encoding/json"
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
	// permit answers session/request_permission; nil selects "allow_once".
	permit func(params map[string]any) map[string]any
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	agent := NewAgent(Deps{Config: cfg, Sessions: config.NewSessionManager(), Home: home, Logf: t.Logf})
	conn := NewConn(inR, outW, agent)
	agent.Attach(conn)
	ctx, cancel := context.WithCancel(context.Background())
	go conn.Serve(ctx)
	c := &testClient{t: t, agent: agent, home: home, in: inW, waiting: map[int]chan rpcReply{}}
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close(); agent.Close() })
	go c.read(bufio.NewReader(outR))
	return c
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
			answer := map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow_once"}}
			if c.permit != nil {
				answer = c.permit(m.Params)
			}
			c.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": answer})
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
