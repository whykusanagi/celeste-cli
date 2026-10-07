// Package atomicfile replaces a file so that a concurrent reader (another
// celeste process, a new lane's setup) sees either the old content or the
// new, never a half-written file, and a crash never leaves a truncated one.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// renameAttempts and retryDelay bound the rename retry: on Windows a rename
// over a file that another process or goroutine has open for reading fails
// with "Access is denied" until the reader closes it.
// ponytail: fixed ~1s of retries, a real lock if saves ever contend longer.
const renameAttempts = 20

var retryDelay = 50 * time.Millisecond

// Write replaces path with data and gives the file mode perm, exactly: the
// umask does not apply, unlike os.WriteFile on a new file. It writes a
// temp file in the target's directory, syncs it, closes it and renames it
// over the target, retrying the rename briefly (see renameAttempts).
//
// path is resolved through any symlinks first (dotfile managers commonly
// symlink ~/.celeste files to ones they track elsewhere): the temp file is
// created next to, and the rename lands on, the real target, so the symlink
// itself survives. The directory must already exist.
func Write(path string, data []byte, perm os.FileMode) error {
	target := path
	if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
		target = resolved
	}
	return replace(target, data, perm)
}

// replace writes data to a temp file next to target, syncs, closes and
// renames it over target. A symlink at target is replaced, not followed.
// The parent directory is opened first and held throughout to prevent TOCTOU
// attacks where an ancestor is replaced with a symlink after validation.
func replace(target string, data []byte, perm os.FileMode) (err error) {
	// Open the parent directory and hold it throughout the operation.
	// This binds to the directory's inode, preventing an attacker from
	// replacing an ancestor with a symlink between validation and use.
	dirHandle, err := openParentDir(target)
	if err != nil {
		return &TempError{Err: err}
	}
	defer dirHandle.close()

	basename := filepath.Base(target)

	// Create temp file using the directory handle (openat on Unix)
	tmpFd, tmpPath, err := dirHandle.createTemp(basename)
	if err != nil {
		return &TempError{Err: err}
	}
	tmpBasename := filepath.Base(tmpPath)

	// Convert fd to *os.File for easier operations
	tmp := os.NewFile(uintptr(tmpFd), tmpPath)
	if tmp == nil {
		return &TempError{Err: fmt.Errorf("failed to create file object")}
	}

	defer func() {
		if err != nil {
			_ = dirHandle.unlinkAt(tmpBasename)
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

	// Rename using the directory handle (renameat on Unix)
	for i := 0; i < renameAttempts; i++ {
		if err = dirHandle.renameTo(tmpBasename, basename); err == nil {
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

// ReplaceKeepMode is WriteKeepMode without following symlinks: the rename
// lands on path itself, so a symlink there is replaced by a regular file
// instead of written through. Callers pass a path they already resolved
// and checked (the edit tools, 2.0 W4), so a symlink swapped in after that
// check cannot redirect the write. A regular file at path keeps its mode;
// anything else gets defaultPerm.
func ReplaceKeepMode(path string, data []byte, defaultPerm os.FileMode) error {
	perm := defaultPerm
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
		perm = fi.Mode().Perm()
	}
	return replace(path, data, perm)
}
