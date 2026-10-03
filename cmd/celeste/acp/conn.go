// Package acp is celeste's Agent Client Protocol agent (2.0 W4): JSON-RPC
// 2.0 over newline-delimited JSON on stdio, for editors such as Zed.
package acp

import (
	"bufio"
	"bytes"
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

// Receiver is an optional Handler hook: Received runs on the read loop when
// a request arrives, before the goroutine that serves it starts, so a
// notification read after the request (session/cancel) is handled knowing
// the request came first. It must not block.
type Receiver interface {
	Received(method string, params json.RawMessage)
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
	// CodeBusy is implementation-defined; ACP v1 already uses -32000 (auth
	// required), -32002 (resource not found) and -32800 (cancelled).
	CodeBusy = -32001
)

// maxLine is the longest incoming line (ruling 1).
const maxLine = 64 << 20

// keepOfLongLine is how much of an oversized line is kept to find its id.
const keepOfLongLine = 4 << 10

// Conn is a bidirectional JSON-RPC 2.0 connection over newline-delimited
// JSON (ACP's framing): it serves incoming requests and notifications to a
// Handler and lets the agent send its own requests and notifications.
type Conn struct {
	// Logf, when set before Serve, logs lines the connection drops (their
	// length and why, never their content) and recovered Notify panics.
	Logf func(format string, args ...any)

	maxLine int
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
	return &Conn{maxLine: maxLine, r: r, w: w, h: h, pending: map[int64]chan response{}, closed: make(chan struct{})}
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

func (c *Conn) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// idKey matches an "id" member; topLevelID keeps only one at depth 1.
var idKey = regexp.MustCompile(`"id"\s*:\s*("[^"\\]*"|-?\d+)`)

// topLevelID finds the id of a line that is not valid JSON, so the error
// can be answered (ruling 1). Only an "id" member of the outermost object
// counts: one nested in params belongs to something else, and answering it
// could fail an unrelated request of the client's.
func topLevelID(line []byte) json.RawMessage {
	matches := idKey.FindAllSubmatchIndex(line, -1)
	if matches == nil {
		return nil
	}
	depth, inString, escaped, mi := 0, false, false, 0
	for i := 0; i < len(line) && mi < len(matches); i++ {
		if !inString && i == matches[mi][0] {
			if depth == 1 {
				m := matches[mi]
				return json.RawMessage(append([]byte(nil), line[m[2]:m[3]]...))
			}
			mi++
		}
		for mi < len(matches) && matches[mi][0] <= i {
			mi++ // this match starts inside a string
		}
		b := line[i]
		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		case inString:
		case b == '{' || b == '[':
			depth++
		case b == '}' || b == ']':
			depth--
		}
	}
	return nil
}

type readResult struct {
	line    []byte
	tooLong bool
	err     error
}

// readLine reads one line without its newline. A line longer than max is
// read to its end but only its first keepOfLongLine bytes are returned.
func readLine(br *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		frag, err := br.ReadSlice('\n')
		if !tooLong {
			line = append(line, frag...)
			if len(line) > max+1 { // +1: the newline
				tooLong, line = true, prefix(line)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		line = bytes.TrimRight(line, "\r\n")
		if !tooLong && len(line) > max {
			tooLong, line = true, prefix(line)
		}
		return line, tooLong, err
	}
}

// prefix keeps the start of an oversized line, enough to find its id.
func prefix(line []byte) []byte {
	return line[:min(len(line), keepOfLongLine):min(len(line), keepOfLongLine)]
}

// readLines feeds Serve until the reader fails or Serve stops listening.
func (c *Conn) readLines(out chan<- readResult, stop <-chan struct{}) {
	br := bufio.NewReaderSize(c.r, 64<<10)
	send := func(r readResult) bool {
		select {
		case out <- r:
			return true
		case <-stop:
			return false
		}
	}
	for {
		line, tooLong, err := readLine(br, c.maxLine)
		if (len(line) > 0 || tooLong) && !send(readResult{line: line, tooLong: tooLong}) {
			return
		}
		if err != nil {
			send(readResult{err: err})
			return
		}
	}
}

// Serve reads messages until EOF or ctx ends; requests run on their own
// goroutines (ruling 2). Pending Calls fail once it returns. When ctx ends
// Serve returns at once; its read goroutine exits when the reader next
// returns (the caller closes it, or the process exits). A line longer than
// the 64 MiB cap is dropped, answered -32600 if its id can be found, and
// the connection keeps serving.
func (c *Conn) Serve(ctx context.Context) error {
	defer close(c.closed)
	lines := make(chan readResult)
	stop := make(chan struct{})
	defer close(stop)
	go c.readLines(lines, stop)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-lines:
			if r.err == io.EOF {
				return nil
			}
			if r.err != nil {
				return r.err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.serveLine(ctx, r.line, r.tooLong)
		}
	}
}

// answerError answers id with an error off the read loop, like every
// response, so a client that writes before it reads cannot stall it.
func (c *Conn) answerError(id json.RawMessage, code int, msg string) {
	answer := map[string]any{"jsonrpc": "2.0", "id": id, "error": &RPCError{Code: code, Message: msg}}
	go func() { _ = c.write(answer) }()
}

func (c *Conn) serveLine(ctx context.Context, line []byte, tooLong bool) {
	if tooLong {
		msg := fmt.Sprintf("invalid request: line longer than %d bytes", c.maxLine)
		if id := topLevelID(line); id != nil {
			c.answerError(id, CodeInvalidRequest, msg)
			return
		}
		c.logf("acp: dropped a line longer than %d bytes", c.maxLine)
		return
	}
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		if id := topLevelID(line); id != nil {
			c.answerError(id, CodeParseError, "parse error: "+err.Error())
			return
		}
		c.logf("acp: dropped a malformed line (%d bytes): %v", len(line), err)
		return
	}
	if m.JSONRPC != "2.0" {
		if m.Method != "" && len(m.ID) > 0 {
			c.answerError(m.ID, CodeInvalidRequest, `invalid request: jsonrpc must be "2.0"`)
			return
		}
		c.logf("acp: dropped a message that is not JSON-RPC 2.0 (%d bytes)", len(line))
		return
	}
	switch {
	case m.Method != "" && len(m.ID) > 0:
		c.received(m)
		go c.serveRequest(ctx, m)
	case m.Method != "":
		c.notify(m)
	case len(m.ID) > 0:
		c.deliver(m)
	}
}

// received runs the handler's Received hook, if it has one, recovering a
// panic.
func (c *Conn) received(m message) {
	r, ok := c.h.(Receiver)
	if !ok {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			c.logf("acp: receiving %s panicked: %v", m.Method, p)
		}
	}()
	r.Received(m.Method, m.Params)
}

// notify runs the Notify handler, recovering a panic as requests do.
func (c *Conn) notify(m message) {
	defer func() {
		if p := recover(); p != nil {
			c.logf("acp: notification %s panicked: %v", m.Method, p)
		}
	}()
	c.h.Notify(m.Method, m.Params)
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
