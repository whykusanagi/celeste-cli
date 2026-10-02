package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreviewTextIsRuneSafe(t *testing.T) {
	got := previewText("aセレステ", 5) // byte 5 falls inside レ
	if !utf8.ValidString(got) || !strings.HasPrefix(got, "aセ\n") {
		t.Fatalf("previewText = %q", got)
	}
	if got := previewText("  セレ  ", 10); got != "セレ" {
		t.Fatalf("short text = %q", got)
	}
}
