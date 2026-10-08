package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
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

// `celeste grimoire` prints repository files; on a terminal their controls
// are escaped.
func TestShowGrimoireIsTerminalSafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("use tabs "+hostileCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := showGrimoire(ws, &out, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "use tabs") {
		t.Fatalf("output:\n%s", out.String())
	}
	assertCLIInert(t, "grimoire", out.String())

	// Piped, like `celeste message`, the text is kept byte for byte.
	out.Reset()
	if err := showGrimoire(ws, &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "use tabs "+hostileCLI) {
		t.Errorf("piped grimoire changed: %q", out.String())
	}
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

// `celeste skill <name>` prints a tool result (a fetched page, a file):
// escaped on a terminal, byte for byte to a pipe; a failure and the skill
// name are always escaped.
func TestPrintSkillResultIsTerminalSafe(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := printSkillResult(&out, &errOut, true, "s", tools.ToolResult{Content: "page\n" + hostileCLI}); code != 0 {
		t.Fatalf("code %d", code)
	}
	assertCLIInert(t, "skill result", out.String())
	if !strings.Contains(out.String(), "page\n") {
		t.Errorf("result lost its line break: %q", out.String())
	}
	out.Reset()
	printSkillResult(&out, &errOut, false, "s", tools.ToolResult{Content: "a\r\n" + hostileCLI})
	if out.String() != "a\r\n"+hostileCLI+"\n" {
		t.Errorf("piped result changed: %q", out.String())
	}
	out.Reset()
	if code := printSkillResult(&out, &errOut, false, "n"+hostileCLI, tools.ToolResult{Error: true, Content: "bad " + hostileCLI}); code != 1 {
		t.Fatalf("failure code %d", code)
	}
	assertCLIInert(t, "skill failure", errOut.String())
	if out.Len() != 0 {
		t.Errorf("failure wrote stdout: %q", out.String())
	}
}

// `celeste session` lists previews of the first message (often pasted file
// or web content) and IDs escaped.
func TestSessionListIsTerminalSafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mgr := config.NewSessionManager()
	s := mgr.NewSession()
	s.Messages = append(s.Messages, config.SessionMessage{Role: "user", Content: "hi " + hostileCLI})
	if err := mgr.Save(s); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := sessionCLI(nil, mgr, &out, &errOut); code != 0 {
		t.Fatalf("code %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Preview:") {
		t.Fatalf("no preview: %q", out.String())
	}
	assertCLIInert(t, "session list", out.String())
}
