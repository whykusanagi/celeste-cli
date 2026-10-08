//go:build unix

package config

import (
	"fmt"
	"os"
	"syscall"
)

// openRegularNoFollow opens path for reading only when it is a regular
// file: never through a symlink in the final component, and without
// blocking on a FIFO or device swapped in after a check.
func openRegularNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return f, nil
}
