package builtin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A symlink inside the workspace must not reach outside it (#187).
func TestResolvePathSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "ws")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{workspace, outside, filepath.Join(workspace, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"escape":      outside,                            // dir link out
		"secret-link": filepath.Join(outside, "secret"),   // file link out
		"dangling":    filepath.Join(outside, "new-file"), // dangling link out
		"inner":       filepath.Join(workspace, "sub"),    // link that stays inside
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(workspace, name)); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		input string
		ok    bool
	}{
		{"file.txt", true},
		{"sub/new/deeper.txt", true}, // nothing exists yet: write_file creates it
		{"inner/file.txt", true},
		{"escape/secret", false},
		{"escape/brand-new.txt", false},
		{"secret-link", false},
		{"dangling", false},
		{"../outside/secret", false},
		{filepath.Join(outside, "secret"), false},
	}
	for _, tc := range cases {
		_, err := resolvePath(workspace, tc.input)
		if (err == nil) != tc.ok {
			t.Errorf("resolvePath(%q): err=%v, want ok=%v", tc.input, err, tc.ok)
		}
	}
}

// A workspace reached through a symlink (macOS /tmp -> /private/tmp) still
// accepts its own files.
func TestResolvePathSymlinkedWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"a.txt", "new.txt", "."} {
		if _, err := resolvePath(link, in); err != nil {
			t.Errorf("resolvePath(link, %q): %v", in, err)
		}
	}
}
