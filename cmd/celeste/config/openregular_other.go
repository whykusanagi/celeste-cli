//go:build !unix

package config

import (
	"fmt"
	"os"
)

// openRegularNoFollow opens path for reading only when it is a regular
// file, the same file Lstat found (no symlink swapped in).
func openRegularNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || !os.SameFile(before, info)) {
		err = fmt.Errorf("%s changed while it was being opened", path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
