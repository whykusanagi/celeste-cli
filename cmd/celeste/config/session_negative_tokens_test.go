package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A session file with a negative token_count (hand-edited or written by a
// buggy build) must not carry the negative count into the context tracker:
// the bar's percentage would go negative.
func TestLoadSanitisesNegativeTokenCount(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	m := NewSessionManager()
	body := `{"id":"neg1","name":"n","messages":[{"role":"user","content":"hello there"}],"token_count":-5000}`
	if err := os.WriteFile(filepath.Join(m.sessionsDir, "neg1.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := m.Load("neg1")
	if err != nil {
		t.Fatal(err)
	}
	if s.TokenCount < 0 {
		t.Fatalf("Load kept negative token_count %d", s.TokenCount)
	}
}

func TestContextTrackerIgnoresNegativeSessionTokenCount(t *testing.T) {
	s := &Session{ID: "x", TokenCount: -5000, Messages: []SessionMessage{{Role: "user", Content: "hello there"}}}
	ct := NewContextTracker(s, "gpt-4o", 128000)
	if ct.CurrentTokens < 0 {
		t.Fatalf("tracker CurrentTokens = %d, want >= 0", ct.CurrentTokens)
	}
	if p := ct.GetUsagePercentage(); p < 0 {
		t.Fatalf("usage percent %v < 0", p)
	}
}

// List feeds resume-by-name, which uses the listed session directly.
func TestListSanitisesNegativeTokenCount(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	m := NewSessionManager()
	body := `{"id":"neg2","name":"n","token_count":-7}`
	if err := os.WriteFile(filepath.Join(m.sessionsDir, "neg2.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	ss, err := m.List()
	if err != nil || len(ss) != 1 {
		t.Fatalf("List = %v, %v", ss, err)
	}
	if ss[0].TokenCount < 0 {
		t.Fatalf("List kept negative token_count %d", ss[0].TokenCount)
	}
}
