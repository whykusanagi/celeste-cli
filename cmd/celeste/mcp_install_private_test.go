package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// MCP client configs can hold server env values: a new one is owner-only,
// and the .bak of an owner-only file is too (Aikido 806869849).
func TestUpsertJSONConfigKeepsConfigsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.json")
	if _, err := upsertJSONConfig(fresh, "celeste", map[string]any{"command": "/c"}, false); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "old.json")
	if err := os.WriteFile(existing, []byte(`{"mcpServers":{"x":{"env":{"T":"v"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing+".bak", []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertJSONConfig(existing, "celeste", map[string]any{"command": "/c"}, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{fresh, existing, existing + ".bak"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
}

// A .bak swapped for a symlink after the symlink check is replaced, not
// written through: the file it points at keeps its content and mode
// (Aikido review of #424).
func TestBackupFileDoesNotFollowASymlinkSwappedIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "client.json")
	if err := os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	testHookBackupChecked = func(bak string) {
		if err := os.Symlink(victim, bak); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookBackupChecked = nil })
	if err := backupFile(config); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("the backup wrote through a symlink: victim %q mode %v", got, fi.Mode().Perm())
	}
	bi, err := os.Lstat(config + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bi.Mode().IsRegular() || bi.Mode().Perm() != 0o600 {
		t.Fatalf(".bak is %v, want a regular 0600 file", bi.Mode())
	}
}

// A client config swapped for a symlink after the symlink check is
// replaced, not written through or chmod'd: the file it points at keeps
// its content and mode (codex review of this branch).
func TestUpsertJSONConfigDoesNotFollowASymlinkSwappedIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "client.json")
	if err := os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victim, 0o644); err != nil {
		t.Fatal(err)
	}
	testHookBackupChecked = func(string) {
		if err := os.Remove(config); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(victim, config); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookBackupChecked = nil })
	if _, err := upsertJSONConfig(config, "celeste", map[string]any{"command": "/c"}, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("the config write went through a symlink: victim %q mode %v", got, fi.Mode().Perm())
	}
	ci, err := os.Lstat(config)
	if err != nil {
		t.Fatal(err)
	}
	if !ci.Mode().IsRegular() || ci.Mode().Perm() != 0o600 {
		t.Fatalf("config is %v, want a regular 0600 file", ci.Mode())
	}
}
