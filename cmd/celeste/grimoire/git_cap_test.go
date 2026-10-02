package grimoire

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapStatusIsRuneSafe(t *testing.T) {
	status := "?? " + strings.Repeat("セ", maxStatusBytes)
	got := capStatus(status)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "... (truncated)") {
		t.Fatalf("capStatus split a character or lost its note")
	}
	if capStatus("M セ.go") != "M セ.go" {
		t.Fatal("short status changed")
	}
}
