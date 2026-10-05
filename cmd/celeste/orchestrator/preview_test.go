package orchestrator

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The review and defense previews are cut at 100 bytes; the cut must not
// split a multibyte character (audit C2's bug class, outside the TUI).
func TestEventPreviewNeverSplitsARune(t *testing.T) {
	s := strings.Repeat("a", 99) + "日本語"
	got := eventPreview(s)
	if !utf8.ValidString(got) {
		t.Fatalf("eventPreview split a rune: %q", got)
	}
	if got != strings.Repeat("a", 99)+"…" {
		t.Fatalf("eventPreview = %q", got)
	}
	if short := eventPreview("short"); short != "short" {
		t.Fatalf("eventPreview(short) = %q", short)
	}
}
