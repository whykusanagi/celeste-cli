//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Aikido 806869299: a workspace .celeste/config.json that is a FIFO, or a
// symlink to one, is refused at once instead of blocking startup.
func TestLoadWorkspaceSandboxRefusesFIFOs(t *testing.T) {
	for _, viaLink := range []bool{false, true} {
		ws := t.TempDir()
		cfg := WorkspaceConfigPath(ws)
		if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
			t.Fatal(err)
		}
		fifo := cfg
		if viaLink {
			fifo = filepath.Join(t.TempDir(), "fifo")
		}
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Skipf("no FIFOs here: %v", err)
		}
		if viaLink {
			if err := os.Symlink(fifo, cfg); err != nil {
				t.Fatal(err)
			}
		}
		done := make(chan error, 1)
		go func() {
			_, _, _, err := LoadWorkspaceSandbox(ws)
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("viaLink=%v: a FIFO was read as the workspace config", viaLink)
			}
		case <-time.After(3 * time.Second):
			// Unblock the reader so the test can finish.
			if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				f.Close()
			}
			t.Fatalf("viaLink=%v: loading the workspace sandbox config blocked on a FIFO", viaLink)
		}
	}
}
