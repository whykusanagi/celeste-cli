//go:build windows

package grimoire

import (
	"os"
)

// openRegular opens path for reading and checks, through the handle, that
// it is a regular file and (checkOpened) the one Lstat saw. Check and open
// are two steps here, so a swap between them is caught only when Lstat saw
// a symlink or a regular file.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err == nil {
		err = checkOpened(path, before, info)
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// openRegularIn is openRegular for rel inside root: no component may lead
// out of root, and the handle must be a regular file (checkOpened).
func openRegularIn(root *os.Root, rel string) (*os.File, error) {
	before, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil {
		err = checkOpened(rel, before, info)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
