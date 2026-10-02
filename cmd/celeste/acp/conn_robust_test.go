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
)

// logSink collects what a Conn logs.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSink) logf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, fmt.Sprintf(format, args...))
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

// Cancelling ctx ends Serve even while it waits on a reader that never
// sends another byte (SIGINT must stop `celeste acp`).
func TestConnServeReturnsWhenContextEnds(t *testing.T) {
	inR, inW := io.Pipe()
	t.Cleanup(func() { inW.Close() })
	c := NewConn(inR, io.Discard, echoHandler{notes: make(chan string, 1)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Serve(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Serve = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve still blocked after ctx was cancelled")
	}
}

// A line over the size cap is dropped and answered -32600 when its id can
// be found; the connection keeps serving.
func TestConnDropsOversizedLines(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	logs := &logSink{}
	c := NewConn(inR, outW, echoHandler{notes: make(chan string, 1)})
	c.Logf = logs.logf
	c.maxLine = 256
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close() })
	go c.Serve(ctx)
	out := newLineReader(outR)

	big := strings.Repeat("x", 100000)
	go func() {
		io.WriteString(inW, `{"jsonrpc":"2.0","id":9,"method":"echo","params":"`+big+`"}`+"\n")
		io.WriteString(inW, `{"jsonrpc":"2.0","method":"session/cancel","params":"`+big+`"}`+"\n")
		io.WriteString(inW, `{"jsonrpc":"2.0","id":10,"method":"echo","params":2}`+"\n")
	}()
	var sawTooLong, sawEcho bool
	for i := 0; i < 2; i++ {
		line := out.next(t)
		switch {
		case strings.Contains(line, `"id":9`) && strings.Contains(line, `-32600`):
			sawTooLong = true
		case strings.Contains(line, `"id":10`) && strings.Contains(line, `"result":2`):
			sawEcho = true
		default:
			t.Fatalf("unexpected line: %.200s", line)
		}
	}
	if !sawTooLong || !sawEcho {
		t.Fatalf("too long %v, echo %v", sawTooLong, sawEcho)
	}
	if got := logs.String(); !strings.Contains(got, "longer than") || strings.Contains(got, "xxxx") {
		t.Fatalf("log must note the dropped line without its content: %q", got)
	}
}

// Only a top-level "id" answers a malformed line: an "id" inside params
// would fail an unrelated request of the client's.
func TestConnMalformedLineIDMustBeTopLevel(t *testing.T) {
	_, out, in := pipePair(t, echoHandler{notes: make(chan string, 1)})
	io.WriteString(in, `{"jsonrpc":"2.0","method":"session/cancel","params":{"id":9},`+"\n")
	io.WriteString(in, `{"jsonrpc":"2.0","method":"x","params":{"text":"\"id\":8"},"id":`+"\n")
	io.WriteString(in, `{"jsonrpc":"2.0","id":11,"method":"echo","params":3}`+"\n")
	line, _ := out.ReadString('\n')
	if !strings.Contains(line, `"id":11`) {
		t.Fatalf("a nested id was answered: %s", line)
	}
}

// Malformed lines that cannot be answered are logged: length and error,
// never the content, which may hold user text.
func TestConnLogsUnanswerableLines(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	logs := &logSink{}
	c := NewConn(inR, outW, echoHandler{notes: make(chan string, 1)})
	c.Logf = logs.logf
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close() })
	go c.Serve(ctx)
	out := newLineReader(outR)
	io.WriteString(inW, `{"method":"secret user text`+"\n")
	io.WriteString(inW, `{"jsonrpc":"2.0","id":12,"method":"echo","params":4}`+"\n")
	out.next(t)
	got := logs.String()
	if !strings.Contains(got, "malformed") || strings.Contains(got, "secret") {
		t.Fatalf("log = %q", got)
	}
}

type panicNotifier struct{ echoHandler }

func (panicNotifier) Notify(string, json.RawMessage) { panic("notify boom") }

// A panicking notification handler is recovered and logged, like requests.
func TestConnRecoversNotifyPanics(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	logs := &logSink{}
	c := NewConn(inR, outW, panicNotifier{echoHandler{notes: make(chan string, 1)}})
	c.Logf = logs.logf
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); inW.Close(); outR.Close() })
	go c.Serve(ctx)
	out := newLineReader(outR)
	io.WriteString(inW, `{"jsonrpc":"2.0","method":"session/cancel","params":{}}`+"\n")
	io.WriteString(inW, `{"jsonrpc":"2.0","id":13,"method":"echo","params":5}`+"\n")
	if line := out.next(t); !strings.Contains(line, `"id":13`) {
		t.Fatalf("after a Notify panic: %s", line)
	}
	if got := logs.String(); !strings.Contains(got, "notify boom") {
		t.Fatalf("panic not logged: %q", got)
	}
}

// CodeBusy must not collide with ACP's own codes (-32000 auth required,
// -32002 resource not found, -32800 request cancelled).
func TestCodeBusyIsFreeInACP(t *testing.T) {
	for _, taken := range []int{-32000, -32002, -32800, CodeParseError, CodeInvalidRequest, CodeMethodNotFound, CodeInvalidParams, CodeInternal} {
		if CodeBusy == taken {
			t.Fatalf("CodeBusy %d collides with a defined code", CodeBusy)
		}
	}
}

type lineReader struct{ lines chan string }

func newLineReader(r io.Reader) *lineReader {
	lr := &lineReader{lines: make(chan string, 16)}
	go func() {
		br := bufio.NewReader(r)
		for {
			s, err := br.ReadString('\n')
			if err != nil {
				close(lr.lines)
				return
			}
			lr.lines <- s
		}
	}()
	return lr
}

func (lr *lineReader) next(t *testing.T) string {
	t.Helper()
	select {
	case s, ok := <-lr.lines:
		if !ok {
			t.Fatal("output closed")
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no answer")
	}
	return ""
}

// readLine caps lines whatever the reader's fragment size, keeps CRLF
// handling, and reads past an oversized line to the next one.
func TestReadLine(t *testing.T) {
	src := strings.Repeat("a", 40) + "\nshort\r\n" + strings.Repeat("b", 21) + "\nlast"
	br := bufio.NewReaderSize(strings.NewReader(src), 16)
	type got struct {
		line    string
		tooLong bool
	}
	var lines []got
	for {
		line, tooLong, err := readLine(br, 20)
		if len(line) > 0 || tooLong {
			lines = append(lines, got{string(line), tooLong})
		}
		if err != nil {
			break
		}
	}
	// An oversized line comes back as a prefix of what was read.
	want := []got{{strings.Repeat("a", 40), true}, {"short", false}, {strings.Repeat("b", 21), true}, {"last", false}}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i, w := range want {
		g := lines[i]
		if g.tooLong != w.tooLong || g.line == "" || !strings.HasPrefix(w.line, g.line) || (!w.tooLong && g.line != w.line) {
			t.Fatalf("line %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestTopLevelID(t *testing.T) {
	for in, want := range map[string]string{
		`{"jsonrpc":"2.0","id":1,"method":`:    `1`,
		`{"id":"a-b","x":`:                     `"a-b"`,
		`{"params":{"id":9},"id":3,`:           `3`,
		`{"method":"m","params":{"id":9},`:     ``,
		`{"params":{"text":"\"id\":8"},`:       ``,
		`{"params":{"text":"}"},"id":4`:        `4`,
		`{"params":[{"id":5}],"x":"\\","id":6`: `6`,
		`not json "id":7`:                      ``,
	} {
		if got := string(topLevelID([]byte(in))); got != want {
			t.Errorf("topLevelID(%s) = %q, want %q", in, got, want)
		}
	}
}
