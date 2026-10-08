package agent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
)

// hostileAgent carries an OSC 52 clipboard write, an OSC 0 title write,
// CR and an erase CSI.
const hostileAgent = "hi\x1b]52;c;Zm9v\x07there\x1b]0;t\x07\r\x1b[2K"

func assertAgentInert(t *testing.T, where, out string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\x07", "\r"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s keeps %q: %q", where, bad, out)
		}
	}
}

// Aikido 806869282: verbose (the default) agent output prints each
// assistant turn, tool name, notice and compaction message live; model and
// tool text reaches the terminal escaped.
func TestVerboseAgentOutputIsTerminalSafe(t *testing.T) {
	var out, errOut bytes.Buffer
	opts := DefaultOptions()
	opts.Verbose = true
	r := &Runner{options: opts, out: &out, errOut: &errOut}
	st := &RunState{Options: opts}
	r.onEvent(st, 0, loop.Event{Kind: loop.EventAssistant, Text: "line one\n" + hostileAgent})
	r.onEvent(st, 0, loop.Event{Kind: loop.EventToolStart, Call: loop.ToolCall{Name: "tool" + hostileAgent}})
	r.onEvent(st, 0, loop.Event{Kind: loop.EventNotice, Text: "n" + hostileAgent})
	r.onEvent(st, 0, loop.Event{Kind: loop.EventCompacted, Text: "c" + hostileAgent})
	r.warning("w" + hostileAgent)
	assertAgentInert(t, "out", out.String())
	assertAgentInert(t, "errOut", errOut.String())
	if !strings.Contains(out.String(), "line one\nhi") {
		t.Errorf("assistant text lost its line break: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Warning: w") {
		t.Errorf("warning missing: %q", errOut.String())
	}
}
