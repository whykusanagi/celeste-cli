package checkpoints

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/atomicfile"
)

// The in-place restore truncates the file before it writes: when that
// write fails, the file gets back what it held, not nothing, and the entry
// stays for another try.
func TestRestoreInPlaceFailureKeepsTheFile(t *testing.T) {
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "before")
	require.NoError(t, snap(sm, f))
	write(t, f, "after, with the user's work")

	oldAtomic, oldWrite := atomicWrite, inPlaceWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return &atomicfile.TempError{Err: os.ErrPermission}
	}
	boom := errors.New("disk full")
	calls := 0
	inPlaceWrite = func(fh *os.File, b []byte) (int, error) {
		calls++
		if calls == 1 {
			n, _ := fh.Write(b[:2]) // a partial write, then the failure
			return n, boom
		}
		return fh.Write(b)
	}
	t.Cleanup(func() { atomicWrite, inPlaceWrite = oldAtomic, oldWrite })

	_, err := sm.RevertLast()
	require.ErrorIs(t, err, boom)
	assert.Equal(t, "after, with the user's work", read(t, f))
	assert.Len(t, sm.Entries(), 1, "the entry stays for another try")
}

// A file whose contents cannot be read cannot be put back after a failed
// in-place write, so it is not written in place at all.
func TestRestoreInPlaceNeedsTheCurrentContents(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads a 0200 file")
	}
	sm, dir := store(t)
	f := filepath.Join(dir, "a.txt")
	write(t, f, "before")
	require.NoError(t, snap(sm, f))
	write(t, f, "after")
	require.NoError(t, os.Chmod(f, 0o200)) // writable, not readable
	t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
	if _, err := os.ReadFile(f); err == nil {
		t.Skip("the filesystem ignores the mode")
	}

	old := atomicWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return &atomicfile.TempError{Err: os.ErrPermission}
	}
	t.Cleanup(func() { atomicWrite = old })

	_, err := sm.RevertLast()
	require.ErrorIs(t, err, os.ErrPermission)
	require.NoError(t, os.Chmod(f, 0o644))
	assert.Equal(t, "after", read(t, f))
}
