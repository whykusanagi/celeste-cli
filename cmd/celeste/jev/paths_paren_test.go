package jev

import (
	"path/filepath"
	"testing"
)

// A path may contain parentheses; the whole path is redacted, and a
// closing parenthesis it does not open stays outside it.
func TestRedactPathsWithParentheses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := filepath.ToSlash(filepath.Join(home, "src", "proj"))
	cases := map[string]string{
		"cat /Users/alice/build(foo)/secret.txt now": "cat <path> now",
		"(see /Users/alice/build(foo)/x.txt).":       "(see <path>).",
		"(/opt/tool/bin)":                            "(<path>)",
		"$(/usr/bin/id)":                             "$(<path>)",
		"edit " + ws + "/a(1).go":                    "edit a(1).go",
		"f(" + ws + "/b.go)":                         "f(b.go)",
		"(" + ws + "/dir(x)/c.go.)":                  "(dir(x)/c.go.)",
	}
	for in, want := range cases {
		if got := RedactPaths(in, ws); got != want {
			t.Errorf("RedactPaths(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
