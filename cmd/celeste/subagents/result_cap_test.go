package subagents

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapSubagentResultIsRuneSafe(t *testing.T) {
	// "a" then 3-byte characters: byte maxResultBytes falls inside one.
	result := "a" + strings.Repeat("セ", maxResultBytes)
	got := capSubagentResult(result)
	if !utf8.ValidString(got) {
		t.Fatal("cap split a UTF-8 character")
	}
	if !strings.HasSuffix(got, "[Result truncated at 100k chars]") {
		t.Fatalf("no truncation note: ...%q", got[len(got)-40:])
	}
	if short := "セレステ"; capSubagentResult(short) != short {
		t.Fatal("a short result changed")
	}
}
