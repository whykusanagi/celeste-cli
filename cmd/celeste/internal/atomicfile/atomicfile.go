// Package atomicfile replaces a file so that a concurrent reader (another
// celeste process, a new lane's setup) sees either the old content or the
// new, never a half-written file, and a crash never leaves a truncated one.
package atomicfile

import (
	"os"
	"path/filepath"
	"time"
)

// renameAttempts and retryDelay bound the rename retry: on Windows a rename
// over a file that another process or goroutine has open for reading fails
// with "Access is denied" until the reader closes it.
// ponytail: fixed ~1s of retries, a real lock if saves ever contend longer.
const renameAttempts = 20

var (
	retryDelay = 50 * time.Millisecond
	rename     = os.Rename // test seam
)

// Write replaces path with data and gives the file mode perm, exactly: the
// umask does not apply, unlike os.WriteFile on a new file. It writes a
// temp file in the target's directory, syncs it, closes it and renames it
// over the target, retrying the rename briefly (see renameAttempts).
//
// path is resolved through any symlinks first (dotfile managers commonly
// symlink ~/.celeste files to ones they track elsewhere): the temp file is
// created next to, and the rename lands on, the real target, so the symlink
// itself survives. The directory must already exist.
func Write(path string, data []byte, perm os.FileMode) (err error) {
	target := path
	if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
		target = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return &TempError{Err: err}
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	// Close before rename: Windows cannot rename an open file.
	if err = tmp.Close(); err != nil {
		return err
	}
	for i := 0; i < renameAttempts; i++ {
		if err = rename(tmpName, target); err == nil {
			return nil
		}
		if i < renameAttempts-1 {
			time.Sleep(retryDelay)
		}
	}
	return err
}

// TempError is Write's error when it could not create its temporary file
// next to the target — typically a directory the process cannot write,
// though the file itself may be writable. Nothing was changed. It reads as
// the underlying error.
type TempError struct{ Err error }

func (e *TempError) Error() string { return e.Err.Error() }
func (e *TempError) Unwrap() error { return e.Err }

// WriteKeepMode is Write that keeps an existing file's mode (following
// symlinks); a new file gets defaultPerm.
func WriteKeepMode(path string, data []byte, defaultPerm os.FileMode) error {
	perm := defaultPerm
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	return Write(path, data, perm)
}
