package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// fitWidth is the TUI's one truncation rule (audit C2): it counts terminal
// cells, never splits a rune, keeps ANSI styling intact, and marks a cut
// with "…".
func TestFitWidthIsCellAndRuneSafe(t *testing.T) {
	styled := "\x1b[1m日本語のテキストです\x1b[0m"
	cases := []struct {
		in   string
		w    int
		want string // "" = only check the invariants
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"日本語テキスト", 7, "日本語…"},
		{"日本語テキスト", 14, "日本語テキスト"},
		{"🔥🔥🔥🔥", 5, "🔥🔥…"},
		{"héllo wörld", 6, "héllo…"},
		{"abc", 1, "…"},
		{"abc", 0, ""},
		{styled, 9, ""},
	}
	for _, tc := range cases {
		got := fitWidth(tc.in, tc.w)
		if tc.want != "" && got != tc.want {
			t.Errorf("fitWidth(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("fitWidth(%q, %d) = %q splits a rune", tc.in, tc.w, got)
		}
		if lipgloss.Width(got) > tc.w {
			t.Errorf("fitWidth(%q, %d) = %q is %d cells", tc.in, tc.w, got, lipgloss.Width(got))
		}
	}
	if got := fitWidth("abcdef\nghijkl", 4); got != "abc…\nghi…" {
		t.Errorf("multi-line: %q, want each line fitted", got)
	}
	if got := fitWidth(styled, 9); !strings.Contains(got, "…") || !strings.HasPrefix(got, "\x1b[1m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("styled cut lost its styling or marker: %q", got)
	}
}

// elideLeft keeps the end of a path, cut on cells and runes too.
func TestElideLeftIsCellAndRuneSafe(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"cmd/celeste/tui/app.go", 30, "cmd/celeste/tui/app.go"},
		{"cmd/celeste/tui/app.go", 10, "…ui/app.go"},
		{"文書/日本語/ファイル.go", 9, ""},
		{"🔥🔥🔥🔥/x.go", 7, ""},
	}
	for _, tc := range cases {
		got := elideLeft(tc.in, tc.w)
		if tc.want != "" && got != tc.want {
			t.Errorf("elideLeft(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
		if !utf8.ValidString(got) || lipgloss.Width(got) > tc.w {
			t.Errorf("elideLeft(%q, %d) = %q (%d cells)", tc.in, tc.w, got, lipgloss.Width(got))
		}
		if lipgloss.Width(tc.in) > tc.w && !strings.HasPrefix(got, "…") {
			t.Errorf("elideLeft(%q, %d) = %q, want a leading …", tc.in, tc.w, got)
		}
	}
}

// The callers that used to slice bytes no longer garble non-ASCII text.
func TestTruncatingCallersKeepUTF8(t *testing.T) {
	long := strings.Repeat("日本語🔥", 40)
	for name, got := range map[string]string{
		"queued": truncateQueued(long),
	} {
		if !utf8.ValidString(got) {
			t.Errorf("%s: %q splits a rune", name, got)
		}
	}
	if w := lipgloss.Width(truncateQueued(long)); w > 80 {
		t.Errorf("queued preview is %d cells, want at most 80", w)
	}
}
