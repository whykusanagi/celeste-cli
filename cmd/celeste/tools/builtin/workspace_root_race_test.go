package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

// The workspace directory, or a directory above it, replaced by a symlink
// between the workspace check and opening it does not move a write out of
// the workspace: the root is opened one component at a time without
// following a symlink (Aikido review of #421).
func TestAtomicWriteRefusesWorkspaceSwappedBeforeOpen(t *testing.T) {
	for _, swapParent := range []bool{false, true} {
		base := realTempDir(t)
		parent := filepath.Join(base, "p")
		ws := filepath.Join(parent, "ws")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
		evil := filepath.Join(base, "evil")
		if err := os.MkdirAll(filepath.Join(evil, "ws"), 0o755); err != nil {
			t.Fatal(err)
		}
		swapped := ws
		target := filepath.Join(evil, "ws")
		if swapParent {
			swapped, target = parent, evil
		}
		testHookBeforeWorkspaceOpen = func() {
			if err := os.Rename(swapped, swapped+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, swapped); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
		err := atomicWrite(ws, filepath.Join(ws, "f.txt"), []byte("x"), 0o644)
		testHookBeforeWorkspaceOpen = nil
		if err == nil {
			t.Errorf("swapParent=%v: write went ahead after the workspace was replaced", swapParent)
		}
		if _, serr := os.Lstat(filepath.Join(evil, "ws", "f.txt")); serr == nil {
			t.Errorf("swapParent=%v: wrote outside the workspace", swapParent)
		}
	}
}

// An unchanged workspace is still written.
func TestAtomicWriteThroughPinnedRoot(t *testing.T) {
	ws := realTempDir(t)
	p := filepath.Join(ws, "sub", "f.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(ws, p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("got %q", b)
	}
}
