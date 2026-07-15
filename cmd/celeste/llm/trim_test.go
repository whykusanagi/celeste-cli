package llm

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func bigLines(n, perLine int) string {
	line := strings.Repeat("x", perLine)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = line
	}
	return strings.Join(parts, "\n")
}

func TestTrimToolResults_TrimsOversizedToolMessageOnLineBoundary(t *testing.T) {
	big := bigLines(500, 100) // ~50 KB across many lines
	orig := []tui.ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: big, Name: "read_file"},
	}
	out, trimmed := trimToolResults(orig, 4096)
	if !trimmed {
		t.Fatal("expected trimming for an oversized tool message")
	}
	if len(out[1].Content) > 4096 {
		t.Fatalf("trimmed content=%d bytes, want <= 4096", len(out[1].Content))
	}
	if !strings.Contains(out[1].Content, "truncated") {
		t.Fatal("trimmed content must carry a recovery notice")
	}
	// Copy-on-write: the caller's slice is untouched.
	if len(orig[1].Content) != len(big) {
		t.Fatal("original message was mutated; must be copy-on-write")
	}
	// Cut lands on a line boundary (before the notice): no partial line.
	head := out[1].Content[:strings.Index(out[1].Content, "\n\n[celeste:")]
	for _, ln := range strings.Split(head, "\n") {
		if ln != "" && len(ln) != 100 {
			t.Fatalf("line-aligned cut expected, got a %d-byte partial line", len(ln))
		}
	}
}

func TestTrimToolResults_LeavesSmallAndNonToolUntouched(t *testing.T) {
	orig := []tui.ChatMessage{
		{Role: "user", Content: bigLines(500, 100)}, // huge but NOT a tool msg
		{Role: "tool", Content: "small result", Name: "search"},
	}
	out, trimmed := trimToolResults(orig, 4096)
	if trimmed {
		t.Fatal("no tool message exceeds budget; should not trim")
	}
	// Same backing slice returned when nothing changes.
	if &out[0] != &orig[0] {
		t.Fatal("expected the original slice to be returned unchanged")
	}
}

// A read_file-sized tool message (~49.6 KiB: 48 KiB content + JSON wrapper) must
// pass the default budget UNtrimmed. If maxToolMsgBytes is ever lowered back to
// read_file's content budget, the pre-flight trim would re-truncate read_file's
// own result and corrupt its metadata JSON (the #1/#2 collision).
func TestTrimToolResults_ReadFileSizedMessagePassesUntrimmed(t *testing.T) {
	msg := tui.ChatMessage{Role: "tool", Name: "read_file", Content: strings.Repeat("x", 49_600)}
	_, trimmed := trimToolResults([]tui.ChatMessage{msg}, maxToolMsgBytes)
	if trimmed {
		t.Fatalf("a ~49.6 KiB read_file message must not be trimmed at the default budget (%d)", maxToolMsgBytes)
	}
}

func TestTrimToolResults_SkipsImageToolResults(t *testing.T) {
	orig := []tui.ChatMessage{
		{Role: "tool", Content: bigLines(500, 100), Metadata: map[string]any{"type": "image"}},
	}
	_, trimmed := trimToolResults(orig, 4096)
	if trimmed {
		t.Fatal("image tool results must not be text-truncated")
	}
}
