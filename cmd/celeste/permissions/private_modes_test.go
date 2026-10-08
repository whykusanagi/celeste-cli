package permissions

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Review: a new permissions file and the directory SaveConfig creates for
// it are owner-only.
func TestSaveConfigIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := filepath.Join(t.TempDir(), "new")
	path := filepath.Join(dir, "permissions.json")
	cfg := DefaultConfig()
	if err := SaveConfig(path, &cfg); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %v, want %v", filepath.Base(p), got, want)
		}
	}
}
