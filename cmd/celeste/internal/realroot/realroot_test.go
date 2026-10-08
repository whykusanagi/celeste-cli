package realroot

import (
	"os"
	"path/filepath"
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
