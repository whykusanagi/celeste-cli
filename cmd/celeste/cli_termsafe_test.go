package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
)

// hostileCLI carries an OSC title write, CR, an erase CSI and a C1 CSI.
const hostileCLI = "x\x1b]0;t\x07\r\x1b[2K\xc2\x9by"

func assertCLIInert(t *testing.T, where, out string) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\x07", "\r", "\xc2\x9b"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s keeps %q: %q", where, bad, out)
		}
	}
}

// Aikido 806869282: headless agent output (the final reply, the error,
// eval case names and reasons) reaches the terminal escaped; the reply
// keeps its line breaks.
func TestAgentOutputIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	printRunSummary(&b, &agent.RunState{RunID: "r1", Status: "done", LastAssistantResponse: "line one\nline two " + hostileCLI, Error: "boom " + hostileCLI})
	assertCLIInert(t, "run summary", b.String())
	if !strings.Contains(b.String(), "line one\nline two") {
		t.Errorf("final reply lost its line break: %q", b.String())
	}
	b.Reset()
	printEvalResults(&b, []agent.EvalResult{{CaseName: "c" + hostileCLI, Status: "s" + hostileCLI, Reason: "r" + hostileCLI}})
	assertCLIInert(t, "eval results", b.String())
}

// Aikido 806869282: setup warnings printed to stderr before the chat
// starts carry workspace paths; they are escaped.
func TestChatWarnSinkIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	s := newChatWarnSink()
	s.stderr = &b
	s.warn("context files: " + hostileCLI + " skipped")
	assertCLIInert(t, "warn sink", b.String())
}

// Memories can be written by the model; `celeste memories` escapes them.
func TestPrintMemoriesIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	printMemories(&b, []*memories.Memory{{Name: "n" + hostileCLI, Description: "d" + hostileCLI, Type: "t" + hostileCLI}})
	assertCLIInert(t, "memories", b.String())
}

// `celeste message` escapes the reply only when stdout is a terminal, so a
// pipe still gets the reply byte for byte.
func TestReplyForTerminal(t *testing.T) {
	if got := replyFor(false, "a\r\nb"+hostileCLI); got != "a\r\nb"+hostileCLI {
		t.Errorf("piped reply changed: %q", got)
	}
	assertCLIInert(t, "terminal reply", replyFor(true, "a\r\nb"+hostileCLI))
}

// `celeste grimoire` prints repository files; their controls are escaped.
func TestShowGrimoireIsTerminalSafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("use tabs "+hostileCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := showGrimoire(ws, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "use tabs") {
		t.Fatalf("output:\n%s", out.String())
	}
	assertCLIInert(t, "grimoire", out.String())
}

// The artifact path in the run summary is shown escaped too.
func TestRunSummaryArtifactPathIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	printRunSummary(&b, &agent.RunState{RunID: "r1", ArtifactBundlePath: "/tmp/a" + hostileCLI})
	assertCLIInert(t, "artifact path", b.String())
}

// `celeste agent --list-runs` shows stored goals escaped.
func TestRunListIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	printRunList(&b, []agent.RunSummary{{RunID: "r1", Status: "s" + hostileCLI, Goal: "goal " + hostileCLI}})
	assertCLIInert(t, "run list", b.String())
}

// Aikido 806869282: a resume or run failure carries provider and model
// error text; it is printed escaped.
func TestAgentFailureIsTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	printAgentFailure(&b, "Agent failed", errors.New("provider said "+hostileCLI))
	assertCLIInert(t, "agent failure", b.String())
	if !strings.HasPrefix(b.String(), "Agent failed: provider said") {
		t.Errorf("failure line: %q", b.String())
	}
}
