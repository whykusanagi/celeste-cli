package codegraph

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Review: the index of a (possibly private) repo lives in an owner-only
// directory.
func TestDefaultIndexPathDirIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Dir(DefaultIndexPath(t.TempDir()))
	for _, p := range []string{dir, filepath.Dir(dir)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o700 {
			t.Errorf("%s mode = %v, want 0700", filepath.Base(p), got)
		}
	}
}
