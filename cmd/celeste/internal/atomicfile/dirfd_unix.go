//go:build unix

package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// dirHandle holds an open directory file descriptor to prevent TOCTOU attacks
// where an ancestor directory is replaced with a symlink after validation.
type dirHandle struct {
	fd   int
	path string
}

// openParentDir opens the parent directory of target and returns a handle.
// The directory must already exist. This binds to the directory's inode,
// preventing an attacker from replacing an ancestor with a symlink after
// the workspace containment check.
func openParentDir(target string) (*dirHandle, error) {
	parent := filepath.Dir(target)

	// Open with O_DIRECTORY to ensure it's a directory, O_RDONLY for minimal
	// permissions, and O_NOFOLLOW to reject a symlink at the final component.
	// This doesn't protect ancestors (the TOCTOU we're fixing), but it does
	// prevent the parent itself from being a symlink swapped in.
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

// createTemp creates a temporary file in the directory represented by this
// handle, using openat to avoid re-traversing the path. Returns the file
// descriptor and the full path to the temporary file.
func (d *dirHandle) createTemp(basename string) (int, string, error) {
	// Generate a unique temporary filename
	pattern := "." + basename + ".tmp-"

	// Try multiple times to create a unique temp file
	for i := 0; i < 10000; i++ {
		// Use a simple counter-based approach since we can't easily use
		// the stdlib's random naming with openat
		tmpName := fmt.Sprintf("%s%d", pattern, os.Getpid()*10000+i)
		tmpPath := filepath.Join(d.path, tmpName)

		// Create exclusively using openat relative to the directory FD
		fd, err := syscall.Openat(d.fd, tmpName,
			syscall.O_RDWR|os.O_CREATE|syscall.O_EXCL, 0600)
		if err == nil {
			return fd, tmpPath, nil
		}

		// If file exists, try next name; otherwise it's a real error
		if err != syscall.EEXIST {
			return -1, "", fmt.Errorf("create temp file: %w", err)
		}
	}

	return -1, "", fmt.Errorf("could not create unique temp file after 10000 attempts")
}

// renameTo renames a file within this directory to target using renameat.
// Both tmpName and target must be basenames (no path separators).
func (d *dirHandle) renameTo(tmpName, targetBasename string) error {
	// Use renameat to rename within the same directory, relative to our FD.
	// This prevents the target path from being re-traversed.
	err := syscall.Renameat(d.fd, tmpName, d.fd, targetBasename)
	if err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, targetBasename, err)
	}
	return nil
}

// unlinkAt removes a file within this directory using unlinkat.
func (d *dirHandle) unlinkAt(basename string) error {
	err := syscall.Unlinkat(d.fd, basename)
	if err != nil && err != syscall.ENOENT {
		return fmt.Errorf("unlink %s: %w", basename, err)
	}
	return nil
}
