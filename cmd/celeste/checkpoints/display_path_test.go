package checkpoints

import (
	"path/filepath"
	"runtime"
	"testing"
)

// DisplayPath shows a path inside the workspace relative to it, and any
// other path as given, by the same lexical check as pathutil.Within.
func TestDisplayPathContainment(t *testing.T) {
	sep := string(filepath.Separator)
	root := t.TempDir()
	d := filepath.Join(root, "ws")
	cases := []struct {
		name, dir, path string
		want            bool
	}{
		{"dir itself", d, d, true},
		{"child", d, filepath.Join(d, "a.txt"), true},
		{"dir trailing separator", d + sep, filepath.Join(d, "a.txt"), true},
		{"path trailing separator", d, filepath.Join(d, "sub") + sep, true},
		{"dot-dot prefixed name", d, filepath.Join(d, "..foo"), true},
		{"unclean but inside", d, d + sep + "sub" + sep + ".." + sep + "a.txt", true},
		{"parent", d, root, false},
		{"sibling sharing the prefix", d, d + "2" + sep + "a.txt", false},
		{"unclean escape", d, d + sep + "sub" + sep + ".." + sep + ".." + sep + "x", false},
		{"relative path against absolute dir", d, "a.txt", false},
		{"other case (lexical, as filepath.Rel)", d, filepath.Join(root, "WS", "a.txt"), runtime.GOOS == "windows"},
	}
	for _, c := range cases {
		want := c.path
		if c.want {
			rel, err := filepath.Rel(c.dir, c.path)
			if err != nil {
				t.Fatal(err)
			}
			want = rel
		}
		if got := DisplayPath(c.dir, c.path); got != want {
			t.Errorf("%s: DisplayPath(%q, %q) = %q, want %q", c.name, c.dir, c.path, got, want)
		}
	}
}
