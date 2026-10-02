package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolvePathReal's lexical containment check (now pathutil.Within) keeps
// its boundaries: "..foo" is a name inside the workspace, a sibling that
// shares the workspace's prefix is outside it, and an unclean path is
// judged once cleaned.
func TestResolvePathContainmentBoundaries(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	cases := []struct {
		name, input string
		escapes     bool
	}{
		{"empty is the workspace", "", false},
		{"dot", ".", false},
		{"child", "a.txt", false},
		{"dot-dot prefixed name", "..foo", false},
		{"unclean but inside", "sub" + sep + ".." + sep + "a.txt", false},
		{"absolute workspace", ws, false},
		{"absolute workspace, trailing separator", ws + sep, false},
		{"absolute child", filepath.Join(ws, "a.txt"), false},
		{"parent", "..", true},
		{"parent child", ".." + sep + "x", true},
		{"unclean escape", "sub" + sep + ".." + sep + ".." + sep + "x", true},
		{"absolute sibling sharing the prefix", ws + "2" + sep + "a.txt", true},
		{"absolute parent", root, true},
	}
	for _, c := range cases {
		_, _, err := resolvePathReal(ws, c.input, false)
		escaped := err != nil && strings.Contains(err.Error(), "escapes workspace")
		if escaped != c.escapes {
			t.Errorf("%s: resolvePathReal(%q) err = %v, want escape %v", c.name, c.input, err, c.escapes)
		}
	}
}
