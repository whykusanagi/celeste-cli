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
