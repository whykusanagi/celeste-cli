package grimoire

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "AGENTS.md" }
func (f fakeInfo) Size() int64        { return 1 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// checkOpened decides from the opened handle whether a context file is a
// plain file; the earlier Lstat only catches a swap. A cloud placeholder
// (OneDrive: a reparse point Lstat calls irregular since Go 1.23) that
// opens as a regular file is read, not skipped.
func TestCheckOpened(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AGENTS.md")
	mk(t, p, "x")
	real, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "CLAUDE.md")
	mk(t, other, "y")
	otherInfo, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		before, opened os.FileInfo
		ok             bool
	}{
		{"plain file, same before and after", real, real, true},
		{"plain file swapped for another", real, otherInfo, false},
		{"symlink swapped in after the checks", fakeInfo{fs.ModeSymlink}, real, false},
		{"cloud placeholder that opens as a file", fakeInfo{fs.ModeIrregular}, real, true},
		{"opens as something else", fakeInfo{fs.ModeIrregular}, fakeInfo{fs.ModeNamedPipe}, false},
		{"opens as a directory", real, fakeInfo{fs.ModeDir}, false},
	}
	for _, c := range cases {
		if err := checkOpened(p, c.before, c.opened); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok %v", c.name, err, c.ok)
		}
	}
}
