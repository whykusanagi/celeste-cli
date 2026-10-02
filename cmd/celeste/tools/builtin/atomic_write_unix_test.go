//go:build unix

package builtin

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Review I2: a new file honours the umask, as os.WriteFile does, so a file
// created under umask 077 is not left readable by other users.
func TestAtomicWriteNewFileHonoursUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	p := filepath.Join(t.TempDir(), "secret.env")
	if err := atomicWrite(p, []byte("k=v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "k=v\n" {
		t.Fatalf("content = %q", b)
	}
}
