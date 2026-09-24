package main

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// celeste resume finds TUI sessions by ID or by name (#188). It used to read a
// store nothing writes.
func TestFindSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	mgr := config.NewSessionManager()
	s := mgr.NewSession()
	s.Name = "Refactor Plans"
	s.Messages = append(s.Messages, config.SessionMessage{Role: "user", Content: "hi"})
	if err := mgr.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	for _, key := range []string{s.ID, "refactor plans"} {
		got, err := findSession(mgr, key)
		if err != nil {
			t.Fatalf("findSession(%q): %v", key, err)
		}
		if got.ID != s.ID {
			t.Errorf("findSession(%q) = %s, want %s", key, got.ID, s.ID)
		}
	}
	if _, err := findSession(mgr, "nope"); err == nil {
		t.Error("findSession should fail for an unknown session")
	}
}
