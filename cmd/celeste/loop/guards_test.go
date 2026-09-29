package loop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func echoTurn(id, args string) fakeprovider.Turn {
	return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "echo", Args: args}}}
}

func TestGuardObserve(t *testing.T) {
	g := guard{limit: 3}
	for i, sig := range []string{"a", "a", "b", "b", "", "b", "b"} {
		if g.observe(sig) {
			t.Fatalf("tripped at %d (%q)", i, sig)
		}
	}
	if !g.observe("b") {
		t.Fatal("the third identical signature in a row must trip")
	}
	if (&guard{}).observe("x") {
		t.Fatal("a zero limit is off")
	}
}

// The third identical call is neither recorded nor executed (MCP and TUI
// behaviour), so the history stays paired.
func TestLoopIdenticalCallGuard(t *testing.T) {
	l, srv := fakeLoop(t, echoTurn("c", `{"k":"x"}`), echoTurn("c", `{"k":"x"}`), echoTurn("c", `{"k":"x"}`), echoTurn("c", `{"k":"x"}`))
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopIdentical || res.Turns != 3 || len(srv.Requests()) != 3 {
		t.Fatalf("res=%+v err=%v requests=%d", res, err, len(srv.Requests()))
	}
	if len(msgs) != 5 {
		t.Fatalf("history has %d messages, want user + 2×(assistant, tool)", len(msgs))
	}
}

func TestLoopIdenticalGuardIgnoresDistinctArgs(t *testing.T) {
	l, _ := fakeLoop(t, echoTurn("c", `{"k":"1"}`), echoTurn("c", `{"k":"2"}`), echoTurn("c", `{"k":"3"}`), fakeprovider.Turn{Text: "done"})
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestLoopProgressGuard(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 8; i++ {
		// Different args, same result: echo ignores "n".
		turns = append(turns, echoTurn("c", fmt.Sprintf(`{"k":"same","n":%d}`, i)))
	}
	l, _ := fakeLoop(t, turns...)
	l.Limits.MaxTurns = 20
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopProgress || res.Turns != 6 {
		t.Fatalf("res=%+v err=%v, want the progress guard at turn 6", res, err)
	}
}

func TestLoopInvalidArgsGuard(t *testing.T) {
	l, _ := fakeLoop(t, echoTurn("c", `{`), echoTurn("c", `{"k"`), echoTurn("c", `{"k":`), fakeprovider.Turn{Text: "unreachable"})
	l.Limits.MaxInvalidArgTurns = 3
	_, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopInvalidArgs || res.Turns != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestLoopTextToolCalls(t *testing.T) {
	block := `<tool_call>{"name":"echo","arguments":{"k":"t"}}</tool_call>`
	l, _ := fakeLoop(t, fakeprovider.Turn{Text: block}, fakeprovider.Turn{Text: "done"})
	l.Limits.TextToolCalls = true
	msgs, res, err := l.Run(context.Background(), userMsg("go"))
	if err != nil || res.StopReason != StopDone || res.ToolCalls != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 0 {
		t.Fatalf("a text-format turn is recorded without native tool_calls: %+v", msgs[1])
	}
	if msgs[2].Role != "user" || !strings.HasPrefix(msgs[2].Content, "[Tool Result: echo]\necho:t") {
		t.Fatalf("msgs[2] = %+v", msgs[2])
	}
}

func TestLoopTextToolCallsOffByDefault(t *testing.T) {
	l, _ := fakeLoop(t, fakeprovider.Turn{Text: `<tool_call>{"name":"echo","arguments":{}}</tool_call>`})
	_, res, _ := l.Run(context.Background(), userMsg("go"))
	if res.StopReason != StopDone || res.ToolCalls != 0 {
		t.Fatalf("res = %+v", res)
	}
}
