package grimoire

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A write that fails after the file was created removes it, so the next
// /init can create it instead of refusing a half-written file.
func TestWriteNewRemovesAPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	boom := errors.New("disk full")
	old := writeContent
	writeContent = func(f *os.File, s string) (int, error) {
		n, _ := f.WriteString(s[:3])
		return n, boom
	}
	t.Cleanup(func() { writeContent = old })
	if err := writeNew(path, "# AGENTS.md\n"); !errors.Is(err, boom) {
		t.Fatalf("writeNew = %v, want the write error", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("partial file left behind: %v", err)
	}
	// An existing file is never removed.
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(path, "x"); !os.IsExist(err) {
		t.Fatalf("writeNew over an existing file = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine" {
		t.Fatalf("existing file changed: %q", b)
	}
}
