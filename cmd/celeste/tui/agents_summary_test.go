package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAgentSummaryLine(t *testing.T) {
	if got := agentSummaryLine("Two issues.\nmore detail"); got != "Two issues.…" {
		t.Fatalf("got %q", got)
	}
	if got := agentSummaryLine("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("セ", 100)
	got := agentSummaryLine(long)
	if !utf8.ValidString(got) || len(got) > 160+len("…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q (%d bytes)", got, len(got))
	}
}
