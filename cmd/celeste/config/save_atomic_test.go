package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Save writes atomically: config.json is private (it can hold an API key),
// new or existing, and an existing one keeps only its owner bits (Aikido
// 806869312).
func TestSaveModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, path, _, _ := Paths()
	if err := Save(&Config{Model: "a"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("new config mode = %v, want 0600", fi.Mode().Perm())
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Config{Model: "b"}); err != nil {
		t.Fatal(err)
	}
	if fi, err = os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("existing config mode = %v, want 0600", fi.Mode().Perm())
	}
}
