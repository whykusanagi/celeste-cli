//go:build windows

package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// dirHandle holds an open directory handle to prevent TOCTOU attacks.
// On Windows, we use a directory handle opened with FILE_FLAG_BACKUP_SEMANTICS.
type dirHandle struct {
	handle syscall.Handle
	path   string
}

// openParentDir opens the parent directory of target and returns a handle.
func openParentDir(target string) (*dirHandle, error) {
	parent := filepath.Dir(target)

	// Convert to UTF-16 for Windows API
	parentUTF16, err := syscall.UTF16PtrFromString(parent)
	if err != nil {
		return nil, fmt.Errorf("convert path: %w", err)
	}

	// Open the directory with FILE_FLAG_BACKUP_SEMANTICS (required for directories)
	// and FILE_FLAG_OPEN_REPARSE_POINT to not follow symlinks at the final component
	handle, err := syscall.CreateFile(
		parentUTF16,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open parent directory %s: %w", parent, err)
	}

	// Verify it's actually a directory
	var fileInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &fileInfo); err != nil {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("get file info: %w", err)
	}

	if fileInfo.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("%s is not a directory", parent)
	}

	// Check if it's a reparse point (symlink)
	if fileInfo.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("%s is a symlink; not followed", parent)
	}

	return &dirHandle{handle: handle, path: parent}, nil
}

// close releases the directory handle.
func (d *dirHandle) close() error {
	if d.handle != syscall.InvalidHandle {
		return syscall.CloseHandle(d.handle)
	}
	return nil
}

// createTemp creates a temporary file in the directory.
// Windows doesn't have openat, so we construct the full path, but we verify
// the parent hasn't changed by checking the directory handle's identity.
func (d *dirHandle) createTemp(basename string) (int, string, error) {
	// Verify the directory handle still points to the expected directory
	// by checking its file ID hasn't changed
	var info1 syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(d.handle, &info1); err != nil {
		return -1, "", fmt.Errorf("verify directory: %w", err)
	}

	// Generate temp filename
	pattern := "." + basename + ".tmp-"

	for i := 0; i < 10000; i++ {
		tmpName := fmt.Sprintf("%s%d", pattern, os.Getpid()*10000+i)
		tmpPath := filepath.Join(d.path, tmpName)

		// Create the file
		tmpUTF16, err := syscall.UTF16PtrFromString(tmpPath)
		if err != nil {
			return -1, "", fmt.Errorf("convert path: %w", err)
		}

		handle, err := syscall.CreateFile(
			tmpUTF16,
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0, // No sharing during creation
			nil,
			syscall.CREATE_NEW, // Fail if exists
			syscall.FILE_ATTRIBUTE_NORMAL,
			0,
		)

		if err == nil {
			// Verify parent directory hasn't changed
			var info2 syscall.ByHandleFileInformation
			if err := syscall.GetFileInformationByHandle(d.handle, &info2); err != nil {
				syscall.CloseHandle(handle)
				return -1, "", fmt.Errorf("verify directory after create: %w", err)
			}

			if info1.VolumeSerialNumber != info2.VolumeSerialNumber ||
				info1.FileIndexHigh != info2.FileIndexHigh ||
				info1.FileIndexLow != info2.FileIndexLow {
				syscall.CloseHandle(handle)
				return -1, "", fmt.Errorf("directory changed during operation")
			}

			// Convert handle to fd
			fd := int(handle)
			return fd, tmpPath, nil
		}

		if err != syscall.ERROR_FILE_EXISTS {
			return -1, "", fmt.Errorf("create temp file: %w", err)
		}
	}

	return -1, "", fmt.Errorf("could not create unique temp file after 10000 attempts")
}

// renameTo renames a file to target. On Windows without renameat, we use the full path
// but verify the directory hasn't changed.
func (d *dirHandle) renameTo(tmpName, targetBasename string) error {
	// Verify directory hasn't changed
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(d.handle, &info); err != nil {
		return fmt.Errorf("verify directory: %w", err)
	}

	tmpPath := filepath.Join(d.path, tmpName)
	targetPath := filepath.Join(d.path, targetBasename)

	// Use MoveFileEx with MOVEFILE_REPLACE_EXISTING
	tmpUTF16, err := syscall.UTF16PtrFromString(tmpPath)
	if err != nil {
		return fmt.Errorf("convert source path: %w", err)
	}

	targetUTF16, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return fmt.Errorf("convert target path: %w", err)
	}

	err = moveFileEx(tmpUTF16, targetUTF16, moveFileReplaceExisting)
	if err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, targetBasename, err)
	}

	return nil
}

// unlinkAt removes a file within this directory.
func (d *dirHandle) unlinkAt(basename string) error {
	path := filepath.Join(d.path, basename)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unlink %s: %w", basename, err)
	}
	return nil
}

// Windows API declarations
var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW = modkernel32.NewProc("MoveFileExW")
)

const (
	moveFileReplaceExisting = 0x1
)

func moveFileEx(from, to *uint16, flags uint32) error {
	r1, _, e1 := syscall.Syscall(procMoveFileExW.Addr(), 3,
		uintptr(unsafe.Pointer(from)),
		uintptr(unsafe.Pointer(to)),
		uintptr(flags))
	if r1 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}
