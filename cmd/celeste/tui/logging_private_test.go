package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Aikido 806869872: the log directory and the day's log hold tool
// arguments and results: owner-only, also when an older version left them
// 0755/0644.
func TestInitLoggingIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".celeste", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("celeste_%s.log", time.Now().Format("2006-01-02")))
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, path} {
		if err := os.Chmod(p, map[bool]os.FileMode{true: 0o755, false: 0o644}[p == dir]); err != nil {
			t.Fatal(err)
		}
	}
	if err := InitLogging(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseLogging)
	for p, want := range map[string]os.FileMode{dir: 0o700, GetLogPath(): 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", filepath.Base(p), fi.Mode().Perm(), want)
		}
	}
}
