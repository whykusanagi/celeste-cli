package prompts

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestComposeUserPromptNil(t *testing.T) {
	if got := ComposeUserPrompt(nil); got != "" {
		t.Fatalf("nil user should return empty, got %q", got)
	}
}

func TestComposeUserPromptKusanagi(t *testing.T) {
	// Kusanagi gets no override — persona handles him
	u := &config.UserIdentity{Name: "Kusanagi"}
	if got := ComposeUserPrompt(u); got != "" {
		t.Fatalf("Kusanagi should return empty override, got %q", got)
	}
}

func TestComposeUserPromptDefault(t *testing.T) {
	u := config.DefaultUserIdentity()
	got := ComposeUserPrompt(u)
	if got == "" {
		t.Fatal("default user (Summoner) should produce a prompt block")
	}
	if !contains(got, "Summoner") {
		t.Fatal("should contain 'Summoner'")
	}
	if !contains(got, "not Kusanagi") {
		t.Fatal("should clarify user is not Kusanagi")
	}
}

func TestComposeUserPromptCustomName(t *testing.T) {
	u := &config.UserIdentity{Name: "Alice"}
	got := ComposeUserPrompt(u)
	if !contains(got, "Alice") {
		t.Fatal("should contain custom name")
	}
	if !contains(got, "not Kusanagi") {
		t.Fatal("should clarify user is not Kusanagi")
	}
	if contains(got, "twin") {
		// The block should tell her NOT to use twin — but it shouldn't contain
		// "twin" itself as an instruction to USE it
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
