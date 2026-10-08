package realroot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenRealDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if b, err := r.ReadFile("f"); err != nil || string(b) != "x" {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
}

func TestOpenRefusesSymlinkOnPath(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(base, "real", "ws")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(base, "wslink")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(base, "link", "ws"), filepath.Join(base, "wslink")} {
		if r, err := Open(p); err == nil {
			r.Close()
			t.Errorf("Open(%s) followed a symlink", p)
		}
	}
}

func TestOpenRefusesFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := Open(f); err == nil {
		r.Close()
		t.Error("file accepted")
	}
}

// A directory above the workspace that can be entered but not listed
// (0o111, as a parent owned by another account often is) does not stop
// Open, and a symlink below it is still refused.
func TestOpenThroughSearchOnlyAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no search-only directories on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(base, "a")
	ws := filepath.Join(a, "b", "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ws, filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(a, "b"), filepath.Join(a, "blink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a, 0o755) })

	r, err := Open(ws)
	if err != nil {
		t.Fatalf("Open under a search-only directory: %v", err)
	}
	if b, err := r.ReadFile("f"); err != nil || string(b) != "x" {
		t.Errorf("ReadFile = %q, %v", b, err)
	}
	r.Close()
	for _, p := range []string{filepath.Join(a, "link"), filepath.Join(a, "blink", "ws")} {
		if r, err := Open(p); err == nil {
			r.Close()
			t.Errorf("Open(%s) followed a symlink below a search-only directory", p)
		}
	}
}

// A search-only workspace itself cannot be opened (os.OpenRoot cannot
// either), and Open says so rather than succeeding.
func TestOpenSearchOnlyWorkspaceFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.Mkdir(ws, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ws, 0o755) })
	if r, err := Open(ws); err == nil {
		r.Close()
		t.Error("search-only workspace opened")
	}
}
