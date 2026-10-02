package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
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

// withinCases are lexical: Within never resolves symlinks or touches the
// filesystem, and it compares case as filepath.Rel does (Windows folds it).
func withinCases(root string) []struct {
	name, dir, path string
	want            bool
} {
	sep := string(filepath.Separator)
	d := filepath.Join(root, "ws")
	return []struct {
		name, dir, path string
		want            bool
	}{
		{"dir itself", d, d, true},
		{"child", d, filepath.Join(d, "a.txt"), true},
		{"nested", d, filepath.Join(d, "sub", "a.txt"), true},
		{"dir trailing separator", d + sep, filepath.Join(d, "a.txt"), true},
		{"path trailing separator", d, filepath.Join(d, "sub") + sep, true},
		{"dot-dot prefixed name", d, filepath.Join(d, "..foo"), true},
		{"unclean but inside", d, d + sep + "sub" + sep + ".." + sep + "a.txt", true},
		{"parent", d, root, false},
		{"sibling", d, filepath.Join(root, "other", "a.txt"), false},
		{"sibling sharing the prefix", d, d + "2" + sep + "a.txt", false},
		{"unclean escape", d, d + sep + "sub" + sep + ".." + sep + ".." + sep + "x", false},
		{"relative path against absolute dir", d, "a.txt", false},
		{"other case", d, filepath.Join(root, "WS", "a.txt"), runtime.GOOS == "windows"},
	}
}

func TestWithin(t *testing.T) {
	for _, c := range withinCases(t.TempDir()) {
		if got := Within(c.dir, c.path); got != c.want {
			t.Errorf("%s: Within(%q, %q) = %v, want %v", c.name, c.dir, c.path, got, c.want)
		}
	}
}

// Within is lexical: a symlink inside dir that points outside still counts
// as inside. Callers that care resolve both paths first.
func TestWithinDoesNotResolveSymlinks(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "ws")
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(d, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if !Within(d, filepath.Join(d, "link", "f")) {
		t.Error("a path through a symlink in dir is lexically inside dir")
	}
}
