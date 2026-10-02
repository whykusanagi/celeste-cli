package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := Write(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != "one" {
		t.Fatalf("got %q", got)
	}
	if err := Write(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != "two" {
		t.Fatalf("got %q", got)
	}
	assertNoTemps(t, dir)
}

func TestWriteSetsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestWriteKeepModePreservesExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil { // umask may have narrowed it
		t.Fatal(err)
	}
	if err := WriteKeepMode(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640 kept", fi.Mode().Perm())
	}

	fresh := filepath.Join(dir, "b.json")
	if err := WriteKeepMode(fresh, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(fresh)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("new file mode = %v, want default 0600", fi.Mode().Perm())
	}
}

func TestWriteThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "dotfiles")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "config.json")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Write(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a plain file")
	}
	if got := readString(t, target); got != "new" {
		t.Fatalf("target = %q, want new", got)
	}
	assertNoTemps(t, dir)
	assertNoTemps(t, realDir)
}

func stubRename(t *testing.T, fn func(from, to string) error) {
	t.Helper()
	oldRename, oldDelay := rename, retryDelay
	rename, retryDelay = fn, time.Millisecond
	t.Cleanup(func() { rename, retryDelay = oldRename, oldDelay })
}

func TestWriteFailedRenameLeavesOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("access is denied")
	calls := 0
	stubRename(t, func(string, string) error { calls++; return boom })
	if err := Write(path, []byte("new"), 0o600); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if calls != renameAttempts {
		t.Fatalf("rename tried %d times, want %d", calls, renameAttempts)
	}
	if got := readString(t, path); got != "old" {
		t.Fatalf("file = %q, want old content intact", got)
	}
	assertNoTemps(t, dir)
}

func TestWriteFailsInMissingDirWithoutCreatingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "a.json")
	if err := Write(path, []byte("x"), 0o600); err == nil {
		t.Fatal("want an error for a missing directory")
	}
}

func TestWriteRetriesTransientRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	calls := 0
	stubRename(t, func(from, to string) error {
		calls++
		if calls < 3 {
			return errors.New("sharing violation")
		}
		return os.Rename(from, to)
	})
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("rename tried %d times, want 3", calls)
	}
	if got := readString(t, path); got != "new" {
		t.Fatalf("got %q", got)
	}
}

// TestWriteWhileReaderHoldsFile is the real race on Windows: a rename over a
// file another handle has open fails until that handle closes. Elsewhere the
// rename simply succeeds at once.
func TestWriteWhileReaderHoldsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		f.Close()
		close(done)
	}()
	err = Write(path, []byte("new"), 0o600)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if got := readString(t, path); got != "new" {
		t.Fatalf("got %q", got)
	}
}

// A temporary file that cannot be created is a *TempError, which reads as
// the underlying error, and nothing is written.
func TestWriteNoTempIsTempError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "a.json")
	err := Write(path, []byte("x"), 0o600)
	var te *TempError
	if !errors.As(err, &te) || !errors.Is(err, os.ErrNotExist) || err.Error() != te.Err.Error() {
		t.Fatalf("err = %#v, want a *TempError wrapping not-exist", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("something was written")
	}
}

func TestReplaceKeepModeReplacesASymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ReplaceKeepMode(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, outside); got != "secret" {
		t.Fatalf("wrote through the symlink: %q", got)
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("path is not a regular file: %v %v", fi, err)
	}
	if got := readString(t, path); got != "new" {
		t.Fatalf("got %q", got)
	}
	assertNoTemps(t, dir)
}

func TestReplaceKeepModeKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceKeepMode(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}
