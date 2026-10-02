package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func TestInitJevWritesThePrivateKeyAndShadowMode(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	home := t.TempDir()
	cfg := &config.Config{JevGate: "on"}
	var out bytes.Buffer
	if err := initJev(cfg, home, strings.NewReader("ts-test-key\n"), &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".celeste", "typesafe.key")
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) != "ts-test-key" {
		t.Fatalf("key file = %q, %v", b, err)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", fi.Mode().Perm())
	}
	if cfg.JevPrune != "shadow" || cfg.JevRoute != "shadow" || cfg.JevGate != "on" {
		t.Errorf("modes = %q %q %q (an \"on\" is never lowered)", cfg.JevPrune, cfg.JevGate, cfg.JevRoute)
	}
	if !strings.Contains(out.String(), "third party") || strings.Contains(out.String(), "ts-test-key") {
		t.Errorf("output must state the third party and never echo the key:\n%s", out.String())
	}
}

func TestInitJevNeedsAKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	home := t.TempDir()
	if err := initJev(&config.Config{}, home, strings.NewReader("\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("no key and no key file must be an error")
	}
	os.MkdirAll(filepath.Join(home, ".celeste"), 0o700)
	os.WriteFile(filepath.Join(home, ".celeste", "typesafe.key"), []byte("old\n"), 0o600)
	cfg := &config.Config{}
	if err := initJev(cfg, home, strings.NewReader("\n"), &bytes.Buffer{}); err != nil || cfg.JevPrune != "shadow" {
		t.Errorf("an existing key file must be kept: %v %+v", err, cfg)
	}
	t.Setenv("TYPESAFE_API_KEY", "from-env")
	if err := initJev(cfg, home, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".celeste", "typesafe.key")); strings.TrimSpace(string(b)) != "from-env" {
		t.Errorf("TYPESAFE_API_KEY must win: %q", b)
	}
}

// The key is replaced atomically: a read-only key file left by the user
// (chmod 400) is replaced, not written into, and the result is 0600.
func TestInitJevReplacesAReadOnlyKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only mode")
	}
	t.Setenv("TYPESAFE_API_KEY", "new-key")
	home := t.TempDir()
	path := filepath.Join(home, ".celeste", "typesafe.key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := initJev(&config.Config{}, home, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) != "new-key" {
		t.Fatalf("key file = %q, %v", b, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("leftover files next to the key: %v", entries)
	}
}
