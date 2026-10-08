package privfs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
}

func TestMkdirAllCreatesAndTightens(t *testing.T) {
	skipWindows(t)
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	if got := perm(t, dir); got != 0o700 {
		t.Fatalf("new dir = %v", got)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	if got := perm(t, dir); got != 0o700 {
		t.Fatalf("existing dir = %v, want 0700", got)
	}
}

func TestTightenKeepsOwnerBits(t *testing.T) {
	skipWindows(t)
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	changed, err := Tighten(p)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := perm(t, p); got != 0o400 {
		t.Fatalf("mode = %v, want 0400", got)
	}
	if changed, err := Tighten(p); err != nil || changed {
		t.Fatalf("second tighten changed=%v err=%v", changed, err)
	}
	if changed, err := Tighten(filepath.Join(t.TempDir(), "missing")); err != nil || changed {
		t.Fatalf("missing: changed=%v err=%v", changed, err)
	}
}

func TestWriteFileModes(t *testing.T) {
	skipWindows(t)
	p := filepath.Join(t.TempDir(), "f")
	if loosened, err := WriteFile(p, []byte("a")); err != nil || loosened {
		t.Fatalf("new: loosened=%v err=%v", loosened, err)
	}
	if got := perm(t, p); got != 0o600 {
		t.Fatalf("new file = %v", got)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if loosened, err := WriteFile(p, []byte("b")); err != nil || !loosened {
		t.Fatalf("existing: loosened=%v err=%v", loosened, err)
	}
	if got := perm(t, p); got != 0o600 {
		t.Fatalf("existing file = %v, want 0600", got)
	}
}

func TestOpenAppendTightens(t *testing.T) {
	skipWindows(t)
	p := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenAppend(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if got := perm(t, p); got != 0o600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
}
