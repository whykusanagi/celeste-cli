package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The opt-in follows the config the session was loaded with, named
// profiles included (Aikido 806869856).
func TestWebFetchPrivateAllowedFollowsSessionConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(WebFetchAllowPrivateEnv, "")
	t.Cleanup(func() { sessionWebFetchAllowPrivate.Store(false) })
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.work.json"), []byte(`{"web_fetch_allow_private": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if WebFetchPrivateAllowed() {
		t.Fatal("off by default")
	}
	if _, err := LoadNamedWithEnv("work"); err != nil {
		t.Fatal(err)
	}
	if !WebFetchPrivateAllowed() {
		t.Fatal("the named profile's opt-in is not honoured")
	}
	if _, err := LoadNamedWithEnv(""); err != nil {
		t.Fatal(err)
	}
	if WebFetchPrivateAllowed() {
		t.Fatal("a profile without the opt-in turns it off")
	}
	t.Setenv(WebFetchAllowPrivateEnv, "1")
	if !WebFetchPrivateAllowed() {
		t.Fatal("the environment opt-in is not honoured")
	}
}
