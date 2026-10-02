package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameSpellings(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if !Same(f, filepath.Join(dir, ".", "sub", "..", "a.txt")) {
		t.Error("clean spellings differ")
	}
	if Same(f, filepath.Join(dir, "b.txt")) {
		t.Error("different files match")
	}
	wd, _ := os.Getwd()
	if !Same("x.txt", filepath.Join(wd, "x.txt")) {
		t.Error("relative and absolute differ")
	}
}

// A symlinked directory (macOS /var -> /private/var) and its target name
// the same file, existing or not.
func TestSameThroughSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !Same(filepath.Join(link, "gone.txt"), filepath.Join(real, "gone.txt")) {
		t.Error("a missing file under a symlinked directory differs")
	}
	f := filepath.Join(real, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Same(filepath.Join(link, "a.txt"), f) {
		t.Error("an existing file under a symlinked directory differs")
	}
}

// Two spellings of one existing file match: letter case on a
// case-insensitive volume (macOS by default), or a hard link.
func TestSameByFileIdentity(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "Name.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "name.txt")); err == nil && !Same(f, filepath.Join(dir, "name.txt")) {
		t.Error("case-insensitive volume: spellings differ")
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Link(f, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if !Same(f, link) || Same(f, filepath.Join(dir, "other.txt")) {
		t.Error("file identity")
	}
}
