//go:build unix

package builtin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// dirHandle holds an open directory file descriptor to prevent TOCTOU attacks.
type dirHandle struct {
	fd   int
	path string
}

// openParentDir opens the parent directory of path and returns a handle.
// This binds to the directory's inode, preventing TOCTOU where an ancestor
// is replaced with a symlink after workspace validation.
func openParentDir(path string) (*dirHandle, error) {
	parent := filepath.Dir(path)

	// Open with O_DIRECTORY and O_NOFOLLOW to ensure it's a directory and
	// reject a symlink at the final component.
	fd, err := syscall.Open(parent, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open parent directory %s: %w", parent, err)
	}

	return &dirHandle{fd: fd, path: parent}, nil
}

// close releases the directory file descriptor.
func (d *dirHandle) close() error {
	if d.fd >= 0 {
		return syscall.Close(d.fd)
	}
	return nil
}

// openFileAt opens a file relative to this directory handle using openat.
// This prevents re-traversal of the path, closing the TOCTOU window.
func (d *dirHandle) openFileAt(basename string, flags int, mode uint32) (*os.File, error) {
	fd, err := syscall.Openat(d.fd, basename, flags, mode)
	if err != nil {
		// Linux and macOS report ELOOP; FreeBSD and NetBSD EMLINK.
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, fmt.Errorf("%s: the file changed to a symlink while it was being opened", basename)
		}
		return nil, err
	}

	fullPath := filepath.Join(d.path, basename)
	return os.NewFile(uintptr(fd), fullPath), nil
}

// openNoFollowSecure opens real using openat relative to a validated parent
// directory, preventing TOCTOU attacks on ancestor directories.
func openNoFollowSecure(real string) (*os.File, error) {
	dirHandle, err := openParentDir(real)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(real)
	return dirHandle.openFileAt(basename, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// openInPlaceSecure opens path for in-place rewrite using openat.
func openInPlaceSecure(path string) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	return dirHandle.openFileAt(basename, os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0)
}

// openAppendSecure opens path for appending using openat.
func openAppendSecure(path string, createMode os.FileMode) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	return dirHandle.openFileAt(basename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, uint32(createMode))
}

// openCreateSecure creates a new file using openat.
func openCreateSecure(path string, mode os.FileMode) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	return dirHandle.openFileAt(basename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, uint32(mode))
}
