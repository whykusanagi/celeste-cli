//go:build unix

package grimoire

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A context file is opened once and checked through that descriptor: a
// FIFO swapped in after the path checks neither blocks the session nor is
// read, and a symlink swapped in is not followed.
func TestOpenRegularRefusesWhatIsNotAPlainFile(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "AGENTS.md")
	mk(t, plain, "# agents\n")
	f, info, err := openRegular(plain)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("plain file: %v", err)
	}
	f.Close()

	fifo := filepath.Join(dir, "fifo", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(fifo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no FIFOs here: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		f, _, err := openRegular(fifo)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was opened as a context file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening a FIFO blocked")
	}

	link := filepath.Join(dir, "link", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}
	if f, _, err := openRegular(link); err == nil {
		f.Close()
		t.Error("a symlink in the final component was followed")
	}
}
