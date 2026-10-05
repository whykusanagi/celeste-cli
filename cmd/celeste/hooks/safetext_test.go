package hooks

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// SafeText is the one rule for showing untrusted text on a terminal (audit
// C4): invalid UTF-8, controls, bidi overrides and other non-printable runes
// are shown Go-quoted; plain text, including non-ASCII, is shown as is.
func TestSafeText(t *testing.T) {
	for in, quoted := range map[string]bool{
		"plain text":        false,
		"日本語 and 🔥":         false,
		"tab\there":         true,
		"esc \x1b[31m red":  true,
		"bidi ‮ evil":       true,
		"isolate ⁦x":        true,
		"zero​width":        true,
		"bad \xff utf-8":    true,
		"c1 \u0085 control": true,
		"line sep":          true,
	} {
		got := SafeText(in)
		if want := map[bool]string{true: strconv.Quote(in), false: in}[quoted]; got != want {
			t.Errorf("SafeText(%q) = %q, want %q", in, got, want)
		}
	}
}

// The approval prompt's MCP summary quotes a line holding invalid UTF-8
// too, not only one with a non-printable rune.
func TestDescribeSourceQuotesInvalidUTF8(t *testing.T) {
	var buf bytes.Buffer
	DescribeSource(&buf, Source{Kind: KindRepoMCP, Rules: "command: ok\nargs: \xff"})
	out := buf.String()
	if !strings.Contains(out, strconv.Quote("args: \xff")) {
		t.Fatalf("invalid UTF-8 line shown raw: %q", out)
	}
	if !strings.Contains(out, "    command: ok\n") {
		t.Fatalf("plain line changed: %q", out)
	}
}
