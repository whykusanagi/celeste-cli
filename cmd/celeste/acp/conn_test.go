package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

type echoHandler struct{ notes chan string }

func (h echoHandler) Request(_ context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "echo":
		return json.RawMessage(params), nil
	case "null":
		return nil, nil
	case "panic":
		panic("boom")
	}
	return nil, &RPCError{Code: CodeMethodNotFound, Message: "no " + method}
}
func (h echoHandler) Notify(method string, _ json.RawMessage) { h.notes <- method }

func pipePair(t *testing.T, h Handler) (*Conn, *bufio.Reader, io.Writer) {
	t.Helper()
	inR, inW := io.Pipe()   // client -> agent
	outR, outW := io.Pipe() // agent -> client
	c := NewConn(inR, outW, h)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close() })
	go c.Serve(ctx)
	return c, bufio.NewReader(outR), inW
}

func TestConnAnswersRequestsAndNotifications(t *testing.T) {
	h := echoHandler{notes: make(chan string, 1)}
	_, out, in := pipePair(t, h)
	io.WriteString(in, `{"jsonrpc":"2.0","id":7,"method":"echo","params":{"x":1}}`+"\n")
	line, _ := out.ReadString('\n')
	if !strings.Contains(line, `"id":7`) || !strings.Contains(line, `"result":{"x":1}`) {
		t.Fatalf("response = %s", line)
	}
	io.WriteString(in, `{"jsonrpc":"2.0","id":"a","method":"nope"}`+"\n")
	line, _ = out.ReadString('\n')
	if !strings.Contains(line, `"id":"a"`) || !strings.Contains(line, `-32601`) {
		t.Fatalf("error response = %s", line)
	}
	io.WriteString(in, `{"jsonrpc":"2.0","method":"session/cancel","params":{}}`+"\n")
	select {
	case m := <-h.notes:
		if m != "session/cancel" {
			t.Fatal(m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestConnCallsTheClient(t *testing.T) {
	c, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	done := make(chan error, 1)
	var got struct{ OK bool }
	go func() {
		done <- c.Call(context.Background(), "session/request_permission", map[string]any{"q": 1}, &got)
	}()
	line, _ := out.ReadString('\n')
	var req struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	json.Unmarshal([]byte(line), &req)
	if req.Method != "session/request_permission" || req.ID == 0 {
		t.Fatalf("request = %s", line)
	}
	io.WriteString(in, `{"jsonrpc":"2.0","id":`+strconv.Itoa(req.ID)+`,"result":{"OK":true}}`+"\n")
	if err := <-done; err != nil || !got.OK {
		t.Fatalf("Call = %v %+v", err, got)
	}
}

// Ruling 1: a malformed line that carries an id gets a parse error; either
// way the connection keeps going. Requests run concurrently (ruling 2), so
// the two answers may come in either order.
func TestConnRejectsMalformedLines(t *testing.T) {
	_, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":`+"\n")
	io.WriteString(in, `not json at all`+"\n")
	io.WriteString(in, `{"jsonrpc":"2.0","id":2,"method":"echo","params":1}`+"\n")
	var sawParseError, sawEcho bool
	for i := 0; i < 2; i++ {
		line, err := out.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(line, `"id":1`) && strings.Contains(line, `-32700`):
			sawParseError = true
		case strings.Contains(line, `"id":2`) && strings.Contains(line, `"result":1`):
			sawEcho = true
		default:
			t.Fatalf("unexpected line: %s", line)
		}
	}
	if !sawParseError || !sawEcho {
		t.Fatalf("parse error %v, echo %v", sawParseError, sawEcho)
	}
}

func TestConnNullResultAndHandlerPanic(t *testing.T) {
	_, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	io.WriteString(in, `{"jsonrpc":"2.0","id":3,"method":"null"}`+"\n")
	line, _ := out.ReadString('\n')
	if !strings.Contains(line, `"id":3`) || !strings.Contains(line, `"result":null`) {
		t.Fatalf("null result = %s", line)
	}
	io.WriteString(in, `{"jsonrpc":"2.0","id":4,"method":"panic"}`+"\n")
	line, _ = out.ReadString('\n')
	if !strings.Contains(line, `"id":4`) || !strings.Contains(line, `-32603`) {
		t.Fatalf("a panicking handler must answer an internal error: %s", line)
	}
}

func TestConnCallErrorsAndClose(t *testing.T) {
	c, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), "x", nil, nil) }()
	line, _ := out.ReadString('\n')
	var req struct {
		ID int `json:"id"`
	}
	json.Unmarshal([]byte(line), &req)
	io.WriteString(in, `{"jsonrpc":"2.0","id":`+strconv.Itoa(req.ID)+`,"error":{"code":-32601,"message":"nope"}}`+"\n")
	err := <-done
	if rerr, ok := err.(*RPCError); !ok || rerr.Code != CodeMethodNotFound {
		t.Fatalf("Call error = %v", err)
	}

	// A call whose context ends returns the context's error.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- c.Call(ctx, "y", nil, nil) }()
	_, _ = out.ReadString('\n')
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("cancelled Call = %v", err)
	}

	// A call pending when the client hangs up returns promptly.
	go func() { done <- c.Call(context.Background(), "z", nil, nil) }()
	_, _ = out.ReadString('\n')
	in.(io.Closer).Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Call after close succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after the connection closed")
	}
}

// A request that is not JSON-RPC 2.0 is answered -32600, not served.
func TestConnRejectsNonJSONRPC2Requests(t *testing.T) {
	_, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	io.WriteString(in, `{"id":5,"method":"echo","params":1}`+"\n")
	line, _ := out.ReadString('\n')
	if !strings.Contains(line, `"id":5`) || !strings.Contains(line, `-32600`) {
		t.Fatalf("non-2.0 request answer = %s", line)
	}
}
