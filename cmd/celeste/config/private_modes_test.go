package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func permOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

// Aikido 806869312: a config file an older version wrote 0644, and a
// ~/.celeste made 0755, are tightened on load, with a warning, and every
// writer keeps them private.
func TestLoadTightensPermissiveConfigFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":      `{"model":"m","base_url":"http://127.0.0.1:1"}`,
		"secrets.json":     `{"api_key":"k"}`,
		"config.work.json": `{"model":"m","base_url":"http://127.0.0.1:1","api_key":"k"}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buf := captureLog(t)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNamed("work"); err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, dir); got != 0o700 {
		t.Errorf("~/.celeste mode = %v, want 0700", got)
	}
	for name := range files {
		if got := permOf(t, filepath.Join(dir, name)); got != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, got)
		}
		if !strings.Contains(buf.String(), name) {
			t.Errorf("no warning names %s: %q", name, buf.String())
		}
	}
}

func TestSaveNamedTightensExistingProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := NamedConfigPath("work")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveNamed("work", &Config{Model: "m", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if got := permOf(t, path); got != 0o600 {
		t.Errorf("profile mode = %v, want 0600", got)
	}
}
