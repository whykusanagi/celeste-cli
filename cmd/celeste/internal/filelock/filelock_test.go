package filelock

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLockExcludesASecondHolderUntilUnlock(t *testing.T) {
	if !Supported {
		t.Skip("no file lock on this platform")
	}
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(path, 50*time.Millisecond); err == nil {
		t.Fatal("a second holder took a held lock")
	}
	got := make(chan error, 1)
	go func() {
		u, err := Lock(path, 5*time.Second)
		if err == nil {
			u()
		}
		got <- err
	}()
	time.Sleep(50 * time.Millisecond)
	unlock()
	if err := <-got; err != nil {
		t.Fatalf("waiting holder did not get the lock after unlock: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("lock file mode %v, want 0600", fi.Mode().Perm())
		}
	}
}
