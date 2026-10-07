//go:build windows

package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// dirHandle holds an open directory handle to prevent TOCTOU attacks.
type dirHandle struct {
	handle syscall.Handle
	path   string
}

// openParentDir opens the parent directory of path and returns a handle.
func openParentDir(path string) (*dirHandle, error) {
	parent := filepath.Dir(path)

	parentUTF16, err := syscall.UTF16PtrFromString(parent)
	if err != nil {
		return nil, fmt.Errorf("convert path: %w", err)
	}

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

	var fileInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &fileInfo); err != nil {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("get file info: %w", err)
	}

	if fileInfo.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("%s is not a directory", parent)
	}

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

// getFileID returns the file ID of the directory for comparison.
func (d *dirHandle) getFileID() (uint32, uint32, uint32, error) {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(d.handle, &info); err != nil {
		return 0, 0, 0, err
	}
	return info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, nil
}

// openFileSecure opens a file in the directory, verifying the directory hasn't changed.
func (d *dirHandle) openFileSecure(basename string, access uint32, shareMode uint32, createDisposition uint32, flags uint32) (*os.File, error) {
	// Get initial directory ID
	vol1, high1, low1, err := d.getFileID()
	if err != nil {
		return nil, fmt.Errorf("verify directory: %w", err)
	}

	fullPath := filepath.Join(d.path, basename)
	pathUTF16, err := syscall.UTF16PtrFromString(fullPath)
	if err != nil {
		return nil, fmt.Errorf("convert path: %w", err)
	}

	handle, err := syscall.CreateFile(
		pathUTF16,
		access,
		shareMode,
		nil,
		createDisposition,
		flags,
		0,
	)
	if err != nil {
		return nil, err
	}

	// Verify directory hasn't changed
	vol2, high2, low2, err := d.getFileID()
	if err != nil {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("verify directory after open: %w", err)
	}

	if vol1 != vol2 || high1 != high2 || low1 != low2 {
		syscall.CloseHandle(handle)
		return nil, fmt.Errorf("directory changed during operation")
	}

	return os.NewFile(uintptr(handle), fullPath), nil
}

// openNoFollowSecure opens real, checking for symlinks and verifying the parent.
func openNoFollowSecure(real string) (*os.File, error) {
	dirHandle, err := openParentDir(real)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(real)
	fullPath := filepath.Join(dirHandle.path, basename)

	// First check with Lstat
	fi, err := os.Lstat(fullPath)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, fmt.Errorf("%s is a symlink or reparse point; not followed", real)
	}

	// Now open it
	return dirHandle.openFileSecure(basename, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL)
}

// openInPlaceSecure opens path for in-place rewrite.
func openInPlaceSecure(path string) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	return dirHandle.openFileSecure(basename, syscall.GENERIC_WRITE,
		0, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL)
}

// openAppendSecure opens path for appending.
func openAppendSecure(path string, createMode os.FileMode) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	f, err := dirHandle.openFileSecure(basename, syscall.GENERIC_WRITE,
		0, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return nil, err
	}

	// Seek to end for append
	if _, err := f.Seek(0, 2); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

// openCreateSecure creates a new file.
func openCreateSecure(path string, mode os.FileMode) (*os.File, error) {
	dirHandle, err := openParentDir(path)
	if err != nil {
		return nil, err
	}
	defer dirHandle.close()

	basename := filepath.Base(path)
	return dirHandle.openFileSecure(basename, syscall.GENERIC_WRITE,
		0, syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL)
}
