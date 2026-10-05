package tui

import (
	"strings"
	"testing"
)

// One list of bare command words (audit C7): what the chat dispatches is
// exactly what a handoff holds back as a command.
func TestLegacyCommandWordsAreOneList(t *testing.T) {
	want := []string{"clear", "help", "tools", "skills", "debug"}
	if len(legacyCommands) != len(want) {
		t.Fatalf("legacyCommands has %d words, want %v", len(legacyCommands), want)
	}
	for _, w := range want {
		if legacyCommands[w] == nil {
			t.Errorf("%q is not dispatched", w)
		}
	}
	for w := range legacyCommands {
		for _, typed := range []string{w, strings.ToUpper(w)} {
			if !isLegacyTextCommand(typed) {
				t.Errorf("isLegacyTextCommand(%q) = false for a dispatched word", typed)
			}
		}
	}
	for _, chat := range []string{"clear the cache", "helpful", "hello"} {
		if isLegacyTextCommand(chat) {
			t.Errorf("isLegacyTextCommand(%q) = true for chat text", chat)
		}
	}
}
