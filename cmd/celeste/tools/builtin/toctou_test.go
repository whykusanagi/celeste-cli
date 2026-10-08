package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// swapAncestor replaces ws/sub with a symlink to a directory outside the
// workspace holding f.txt, as a process racing the write tools could after
// resolvePathReal checked ws/sub/f.txt. It returns the outside file.
func swapAncestor(t *testing.T, ws string) string {
	t.Helper()
	outside := filepath.Join(t.TempDir(), "outside")
	put(t, filepath.Join(outside, "f.txt"), "OUTSIDE")
	put(t, filepath.Join(ws, "sub", "f.txt"), "inside")
	if err := os.Rename(filepath.Join(ws, "sub"), filepath.Join(ws, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "sub")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return filepath.Join(outside, "f.txt")
}

// Aikido 806869649: a whole-file write to a path checked inside the
// workspace cannot land outside it when an ancestor directory is swapped
// for a symlink after the check.
func TestAtomicWriteRefusesSwappedAncestor(t *testing.T) {
	ws := realTempDir(t)
	real := filepath.Join(ws, "sub", "f.txt")
	outside := swapAncestor(t, ws)
	_ = atomicWrite(ws, real, []byte("PAYLOAD"), 0o644)
	if got := get(t, outside); got != "OUTSIDE" {
		t.Fatalf("the write landed outside the workspace: %q", got)
	}
}

// realTempDir is t.TempDir with symlinks resolved (macOS /var), as
// resolvePathReal's real paths are.
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Aikido 806869649: write_file, appending or overwriting, does not follow
// an ancestor directory swapped for a symlink after its path checks.
func TestWriteFileRefusesAncestorSwappedAfterCheck(t *testing.T) {
	for _, appendMode := range []bool{true, false} {
		ws := realTempDir(t)
		put(t, filepath.Join(ws, "sub", "f.txt"), "inside")
		var outside string
		orig := afterPathCheck
		afterPathCheck = func(string) {
			_ = os.RemoveAll(filepath.Join(ws, "sub"))
			outside = swapAncestor(t, ws)
		}
		res := run(t, NewWriteFileTool(ws), context.Background(), map[string]any{
			"path": "sub/f.txt", "content": "PAYLOAD", "append": appendMode,
		})
		afterPathCheck = orig
		if got := get(t, outside); got != "OUTSIDE" {
			t.Fatalf("append=%v: the write landed outside the workspace: %q", appendMode, got)
		}
		if !res.Error {
			t.Errorf("append=%v: the write reported success: %s", appendMode, res.Content)
		}
	}
}
