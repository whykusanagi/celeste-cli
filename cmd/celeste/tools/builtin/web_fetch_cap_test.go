package builtin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapFetchedIsRuneSafe(t *testing.T) {
	content := "ab" + strings.Repeat("セ", maxFetchBytes)
	got := capFetched(content)
	if !utf8.ValidString(got) {
		t.Fatal("cap split a UTF-8 character")
	}
	if !strings.HasSuffix(got, "[Content truncated at 32KB]") || len(got) > maxFetchBytes+40 {
		t.Fatalf("bad cap: %d bytes", len(got))
	}
	if short := "セレステ"; capFetched(short) != short {
		t.Fatal("short content changed")
	}
}
