package builtin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Review: the task list and the directory it creates are owner-only.
func TestTodoStoreFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	ws := t.TempDir()
	NewTodoStore(ws).Create("t", "d")
	dir := filepath.Join(ws, ".celeste")
	for p, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "tasks.json"): 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %v, want %v", filepath.Base(p), got, want)
		}
	}
}
