package checkpoints

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A file that cannot be stat'ed (a directory the process may not search)
// reports that error, not "read it first": reading again would fail the
// same way and the model would never learn why.
func TestCheckReadReportsStatErrors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := NewFileTracker().CheckRead(f)
	if err == nil || errors.Is(err, ErrNotRead) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("CheckRead = %v, want the permission error", err)
	}
}
