// Package acp is celeste's Agent Client Protocol agent (2.0 W4): JSON-RPC
// 2.0 over newline-delimited JSON on stdio, for editors such as Zed.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"sync/atomic"
)

// Handler serves the requests and notifications a Conn reads.
type Handler interface {
	// Request answers a request; return (nil, nil) for a null result.
	// Each request runs on its own goroutine.
	Request(ctx context.Context, method string, params json.RawMessage) (any, *RPCError)
	// Notify handles a notification. It runs on the read loop, so it must
	// not block (session/cancel has to get through while prompts run).
	Notify(method string, params json.RawMessage)
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

// JSON-RPC error codes celeste answers with.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeInvalidParams  = -32602
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
	CodeBusy           = -32002
)

// maxLine is the longest incoming line (ruling 1).
const maxLine = 64 << 20

// Conn is a bidirectional JSON-RPC 2.0 connection over newline-delimited
// JSON (ACP's framing): it serves incoming requests and notifications to a
// Handler and lets the agent send its own requests and notifications.
type Conn struct {
	r       io.Reader
	w       io.Writer
	h       Handler
	wmu     sync.Mutex
	nextID  atomic.Int64
	pmu     sync.Mutex
	pending map[int64]chan response
	closed  chan struct{}
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    *RPCError
}

// NewConn returns a connection that reads r and writes w; Serve starts it.
func NewConn(r io.Reader, w io.Writer, h Handler) *Conn {
	return &Conn{r: r, w: w, h: h, pending: map[int64]chan response{}, closed: make(chan struct{})}
}

// write sends one message as one line; writes are serialized (ruling 2).
func (c *Conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// idOfMalformed finds the id of a line that is not valid JSON, so the
// parse error can be answered (ruling 1).
var idOfMalformed = regexp.MustCompile(`"id"\s*:\s*("[^"\\]*"|-?\d+)`)

// Serve reads messages until EOF or ctx ends; requests run on their own
// goroutines (ruling 2). Pending Calls fail once it returns.
func (c *Conn) Serve(ctx context.Context) error {
	defer close(c.closed)
	sc := bufio.NewScanner(c.r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			if id := idOfMalformed.FindSubmatch(line); id != nil {
				// Answered off the read loop, like every response, so a
				// client that writes before it reads cannot stall it.
				answer := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(append([]byte(nil), id[1]...)),
					"error": &RPCError{Code: CodeParseError, Message: "parse error: " + err.Error()}}
				go func() { _ = c.write(answer) }()
			}
			continue
		}
		if m.JSONRPC != "2.0" {
			if m.Method != "" && len(m.ID) > 0 {
				answer := map[string]any{"jsonrpc": "2.0", "id": m.ID,
					"error": &RPCError{Code: CodeInvalidRequest, Message: `invalid request: jsonrpc must be "2.0"`}}
				go func() { _ = c.write(answer) }()
			}
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			go c.serveRequest(ctx, m)
		case m.Method != "":
			c.h.Notify(m.Method, m.Params)
		case len(m.ID) > 0:
			c.deliver(m)
		}
	}
	return sc.Err()
}

func (c *Conn) serveRequest(ctx context.Context, m message) {
	res, rerr := c.callHandler(ctx, m)
	out := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if rerr == nil {
		b, err := json.Marshal(res) // nil marshals as null
		if err != nil {
			rerr = &RPCError{Code: CodeInternal, Message: "encoding the result: " + err.Error()}
		} else {
			out["result"] = json.RawMessage(b)
		}
	}
	if rerr != nil {
		out["error"] = rerr
	}
	_ = c.write(out) // a failed write means the client is gone
}

// callHandler runs the handler, turning a panic into an internal error so
// one bad request cannot take the agent down.
func (c *Conn) callHandler(ctx context.Context, m message) (res any, rerr *RPCError) {
	defer func() {
		if p := recover(); p != nil {
			res, rerr = nil, &RPCError{Code: CodeInternal, Message: fmt.Sprintf("internal error: %v", p)}
		}
	}()
	return c.h.Request(ctx, m.Method, m.Params)
}

func (c *Conn) deliver(m message) {
	var id int64
	if json.Unmarshal(m.ID, &id) != nil {
		return
	}
	c.pmu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.pmu.Unlock()
	if ok {
		ch <- response{result: m.Result, err: m.Error}
	}
}

// Call sends a request to the client and waits for its answer, the end of
// ctx, or the connection closing.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan response, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("acp: connection closed")
	}
}

// Notify sends a notification to the client.
func (c *Conn) Notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
