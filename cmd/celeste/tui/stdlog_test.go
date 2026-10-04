package tui

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// While the chat program runs, the standard library's log package must not
// reach the terminal (V3): tools/mcp's log.Printf calls drew over the frame.
// RedirectStdLog sends it to the TUI log file and its restore func puts the
// previous writer back.
func TestRedirectStdLogSendsLogToTheLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var term bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&term)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	if err := InitLogging(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseLogging)

	restore := RedirectStdLog()
	log.Printf("[mcp] connected to %d server(s)", 2)
	if term.Len() != 0 {
		t.Fatalf("log output reached the terminal while the TUI runs: %q", term.String())
	}
	data, err := os.ReadFile(GetLogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[mcp] connected to 2 server(s)") {
		t.Fatalf("the log file lacks the entry:\n%s", data)
	}

	restore()
	log.Printf("after exit")
	if !strings.Contains(term.String(), "after exit") {
		t.Fatalf("restore did not put the previous writer back: %q", term.String())
	}
}

// With no log file open, the output is dropped rather than drawn.
func TestRedirectStdLogDiscardsWithoutALogFile(t *testing.T) {
	CloseLogging()
	var term bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&term)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	restore := RedirectStdLog()
	defer restore()
	log.Printf("dropped")
	if term.Len() != 0 {
		t.Fatalf("log output reached the terminal: %q", term.String())
	}
}
