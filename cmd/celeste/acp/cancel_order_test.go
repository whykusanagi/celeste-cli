package acp

import (
	"bytes"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// cancelOnWrite calls onMatch (once) before it writes a chunk holding
// match, so a session/cancel lands while that write is in flight.
type cancelOnWrite struct {
	w       io.Writer
	match   []byte
	onMatch atomic.Pointer[func()]
}

func (c *cancelOnWrite) Write(p []byte) (int, error) {
	if bytes.Contains(p, c.match) {
		if f := c.onMatch.Swap(nil); f != nil {
			(*f)()
		}
	}
	return c.w.Write(p)
}

// A prompt decides whether it was cancelled before it sends a guard's
// "Stopped: ... send another message" notice (CodeRabbit, #412): a cancel
// that arrives while the notice is being sent no longer turns the answer
// into "cancelled", so the editor is never told both.
func TestCancelDuringTheGuardNoticeIsNotAnsweredCancelled(t *testing.T) {
	call := fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "l", Name: "list_files", Args: `{}`}}}
	srv := fakeprovider.NewOpenAI(t, call, call, call, call, fakeprovider.Turn{Text: "next"})
	cw := &cancelOnWrite{match: []byte("identical tool call")}
	c := newTestClientOut(t, testConfig(srv, 0), t.TempDir(), func(w io.Writer) io.Writer {
		cw.w = w
		return cw
	})
	sid := c.newSession(t.TempDir())
	s := c.agent.session(sid)
	cancel := func() { s.cancelPrompt() }
	cw.onMatch.Store(&cancel)

	select {
	case r := <-c.callAsync("session/prompt", textPrompt(sid, "loop")):
		if r.err != nil {
			t.Fatalf("prompt error = %v", r.err)
		}
		notice := strings.Contains(c.agentText(), "identical tool call")
		if !notice {
			t.Fatalf("the guard's notice was not sent: %q", c.agentText())
		}
		if got := stopReason(t, r.result); got != "end_turn" {
			t.Fatalf("stop reason = %q after the notice was sent; want end_turn", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the prompt never answered")
	}
}

// A provider error on a turn a cancel also reached is answered
// "cancelled", and the error goes to the operator log instead of being
// dropped (CodeRabbit, #412).
func TestCancelledTurnLogsItsProviderError(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"model exploded"}}`},
		fakeprovider.Turn{Text: "fine now"})
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	s := c.agent.session(sid)
	c.agent.storeMu.Lock()
	locked := true
	defer func() {
		if locked {
			c.agent.storeMu.Unlock()
		}
	}()
	ch := c.callAsync("session/prompt", textPrompt(sid, "first"))
	deadline := time.Now().Add(30 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.history)
		s.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop never returned")
		}
		time.Sleep(time.Millisecond)
	}
	c.notify("session/cancel", map[string]any{"sessionId": sid})
	if _, err := c.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	c.agent.storeMu.Unlock()
	locked = false
	select {
	case r := <-ch:
		if r.err != nil || stopReason(t, r.result) != "cancelled" {
			t.Fatalf("prompt = %s %v; want cancelled", r.result, r.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the prompt never answered")
	}
	if !c.logged("model exploded") {
		t.Fatal("the cancelled turn's provider error was not logged")
	}
}
