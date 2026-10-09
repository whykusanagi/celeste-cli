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

// A search-only ancestor swapped for a symlink while Open walks below it by
// path is detected: the directory Open returns must really sit under the
// ancestor it checked, so the swap fails the open instead of rooting it in
// the directory the symlink names.
func TestOpenSearchOnlyAncestorSwapRefused(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(base, "a")
	ws := filepath.Join(a, "b", "ws")
	evil := filepath.Join(base, "evil")
	for _, d := range []string{ws, filepath.Join(evil, "b", "ws")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(evil, "b", "ws", "marker"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a, 0o111); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "a.old")
	t.Cleanup(func() {
		_ = os.Chmod(a, 0o755)
		_ = os.Chmod(moved, 0o755)
	})
	swapped := false
	testHookPathStep = func(path string) {
		if swapped || path != filepath.Join(a, "b") {
			return
		}
		swapped = true
		if err := os.Rename(a, moved); err != nil {
			t.Errorf("rename: %v", err)
			return
		}
		if err := os.Symlink(evil, a); err != nil {
			t.Errorf("symlink: %v", err)
		}
	}
	t.Cleanup(func() { testHookPathStep = nil })

	r, err := Open(ws)
	if !swapped {
		t.Fatal("hook never ran")
	}
	if err == nil {
		b, _ := r.ReadFile("marker")
		r.Close()
		t.Fatalf("Open followed a swapped search-only ancestor (marker %q)", b)
	}
}

// Two search-only directories in a row are both checked by ancestry and
// the workspace below them still opens.
func TestOpenThroughNestedSearchOnlyAncestors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(base, "a")
	b := filepath.Join(a, "b")
	ws := filepath.Join(b, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{b, a} {
		if err := os.Chmod(d, 0o111); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = os.Chmod(a, 0o755)
		_ = os.Chmod(b, 0o755)
	})
	r, err := Open(ws)
	if err != nil {
		t.Fatalf("Open under nested search-only directories: %v", err)
	}
	r.Close()
}
